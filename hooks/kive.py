# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

"""Client for the kive hook runtime API (KV store, demands, worker SSH, HTTP proxy)."""

import json
import os
import re
import subprocess
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

WORKER_INSTALL_ROOT = "/opt/kive"
_RUNTIME_API_DEFAULT_PORT = 8080
_ROUTE_STORE_KEYS = "/kv"
_ROUTE_STORE_KEYS_LIST = "/kv/keys"
_ROUTE_STORE_SECRET = "/kv/secret"
_ROUTE_DEMANDS = "/demands"
_ROUTE_SEMAPHORE_ACQUIRE = "/semaphore/acquire"
_ROUTE_SEMAPHORE_RELEASE = "/semaphore/release"
_ROUTE_SEMAPHORE_STATUS = "/semaphore/status"
_ROUTE_HTTP = "/http"


class Response:
    """Minimal requests-compatible response for runtime API calls."""

    def __init__(self, status_code, body, headers=None, url=""):
        self.status_code = status_code
        self._body = body if isinstance(body, (bytes, bytearray)) else body.encode("utf-8")
        self.url = url
        self.headers = headers or {}

    @property
    def ok(self):
        return 200 <= self.status_code < 300

    @property
    def text(self):
        return self._body.decode("utf-8", errors="replace")

    @property
    def content(self):
        return self._body

    def json(self):
        return json.loads(self.text)

    def raise_for_status(self):
        if self.status_code >= 400:
            raise urllib.error.HTTPError(
                self.url,
                self.status_code,
                self.text,
                hdrs=None,
                fp=None,
            )


def allocation_id():
    return os.environ.get("ALLOCATION_ID")


def allocation_ip():
    return os.environ.get("ALLOCATION_IP")


def allocation_index():
    return os.environ.get("ALLOCATION_INDEX")


def is_allocation_disabled():
    return os.environ.get("DISABLED") == "1"


def is_one_shot():
    """True on the first allocation of the first batch.

    Prefer BATCH_ALLOCATIONS[0] over ALLOCATION_INDEX=0: rollout order can put
    catalog index 0 in a later batch, which would otherwise make no worker one-shot.
    Use for logic that must run exactly once per kive invocation."""
    if os.environ.get("BATCH_INDEX", "0") != "0":
        return False
    batch = [
        ip.strip()
        for ip in os.environ.get("BATCH_ALLOCATIONS", "").split(",")
        if ip.strip()
    ]
    if batch:
        return (os.environ.get("ALLOCATION_IP") or "").strip() == batch[0]
    return os.environ.get("ALLOCATION_INDEX", "0") == "0"


def hook_event():
    return os.environ.get("EVENT")


def hook_name():
    return os.environ.get("HOOK")


def job_name():
    return os.environ.get("JOB")


def _runtime_api_base_url():
    host = os.environ.get("HOOK_API_HOST", "127.0.0.1")
    port = os.environ.get("HOOK_API_PORT", str(_RUNTIME_API_DEFAULT_PORT))
    return f"http://{host}:{port}"


def _runtime_request_headers():
    headers = {
        "X-ALLOCATION-ID": allocation_id() or "",
        "HOOK": hook_name() or "",
        "EVENT": hook_event() or "",
        "Content-Type": "application/json",
        "Accept": "application/json",
    }
    token = os.environ.get("HOOK_API_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def _runtime_request(method, path, body=None, query=None, timeout=120):
    url = f"{_runtime_api_base_url()}{path}"
    if query:
        url = f"{url}?{urllib.parse.urlencode(query)}"
    data = None
    if body is not None:
        data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers=_runtime_request_headers(),
        method=method,
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return Response(resp.getcode(), resp.read(), dict(resp.headers), url)
    except urllib.error.HTTPError as exc:
        payload = exc.read() if exc.fp is not None else b""
        return Response(exc.code, payload, dict(exc.headers or {}), url)


def get_store_value(namespace, key):
    """GET /kv — read a key from an allowed namespace."""
    return _runtime_request(
        "GET",
        _ROUTE_STORE_KEYS,
        body={"namespace": namespace, "key": key},
    )


def put_rollout_order(order):
    """PUT rollout_order for the current job (pre_deploy or cli only).

    order: comma-separated worker IPs, or a list/tuple of IPs.
    """
    if isinstance(order, (list, tuple)):
        order = ",".join(str(ip).strip() for ip in order if str(ip).strip())
    return _runtime_request(
        "PUT",
        _ROUTE_STORE_KEYS,
        body={
            "namespace": f"kive/job/{job_name()}",
            "key": "rollout_order",
            "value": order,
        },
    )


def get_rollout_order():
    """GET rollout_order for the current job."""
    return get_store_value(f"kive/job/{job_name()}", "rollout_order")


def put_job_variable(key, value, ttl=0):
    """PUT /kv — write a key under vars/job/<current job>. ttl is optional seconds (0 = no expiry)."""
    body = {
        "namespace": f"vars/job/{job_name()}",
        "key": key,
        "value": value,
    }
    if ttl:
        body["ttl"] = ttl
    return _runtime_request("PUT", _ROUTE_STORE_KEYS, body=body)


def list_job_keys(namespace=None):
    """GET /kv/keys — list keys under vars/job/<job> and secrets/job/<job>."""
    body = {}
    if namespace is not None:
        body["namespace"] = namespace
    return _runtime_request("GET", _ROUTE_STORE_KEYS_LIST, body=body)


def delete_job_variable(key):
    """DELETE /kv — remove a key under vars/job/<current job>."""
    return _runtime_request(
        "DELETE",
        _ROUTE_STORE_KEYS,
        body={
            "namespace": f"vars/job/{job_name()}",
            "key": key,
        },
    )


def delete_job_secret(key):
    """DELETE /kv/secret — remove an encrypted key under secrets/job/<current job>."""
    return _runtime_request(
        "DELETE",
        _ROUTE_STORE_SECRET,
        body={
            "namespace": f"secrets/job/{job_name()}",
            "key": key,
        },
    )


def put_job_secret(key, value, ttl=0):
    """PUT /kv/secret — write an encrypted key under secrets/job/<current job>. ttl is optional seconds."""
    body = {
        "namespace": f"secrets/job/{job_name()}",
        "key": key,
        "value": value,
    }
    if ttl:
        body["ttl"] = ttl
    return _runtime_request("PUT", _ROUTE_STORE_SECRET, body=body)


def list_hook_demands():
    """GET /demands — list hooks that depend on this hook."""
    return _runtime_request("GET", _ROUTE_DEMANDS)


def acquire_semaphore(name, capacity=1, timeout_seconds=600):
    """POST /semaphore/acquire — block until this allocation holds a slot.

    Use capacity=1 for a leader/mutex (e.g. deploy one allocation before the rest).
    Scoped per job and hook event (pre_deploy, post_deploy, etc.).
    """
    return _runtime_request(
        "POST",
        _ROUTE_SEMAPHORE_ACQUIRE,
        body={
            "name": name,
            "capacity": capacity,
            "timeout_seconds": timeout_seconds,
        },
        timeout=timeout_seconds + 30,
    )


def release_semaphore(name):
    """POST /semaphore/release — release a slot held by this allocation."""
    return _runtime_request(
        "POST",
        _ROUTE_SEMAPHORE_RELEASE,
        body={"name": name},
    )


def semaphore_status(name):
    """GET /semaphore/status — inspect holders and waiters for a named semaphore."""
    return _runtime_request(
        "GET",
        _ROUTE_SEMAPHORE_STATUS,
        query={"name": name},
    )


def http_request(method, url, headers=None, body="", timeout_seconds=30, tls=None):
    """POST /http — outbound HTTP(S) via the Go runtime API (worker-IP allowlist).

    Returns a dict with status_code, headers, body, truncated, and optional error.
    Raises urllib.error.HTTPError when the runtime API itself returns 4xx/5xx
    (policy/auth/upstream dial failures), except when the proxy returns JSON
    with an upstream status_code (HTTP 200 from the runtime API).
    """
    payload = {
        "method": method,
        "url": url,
        "headers": headers or {},
        "body": body or "",
        "timeout_seconds": timeout_seconds,
        "tls": tls or {},
    }
    # Upstream may take up to timeout_seconds; allow a small buffer for the proxy.
    resp = _runtime_request(
        "POST",
        _ROUTE_HTTP,
        body=payload,
        timeout=timeout_seconds + 30,
    )
    if resp.status_code >= 400:
        resp.raise_for_status()
    return resp.json()


# Legacy aliases for older hook scripts.
def get_allocation_id():
    return allocation_id()


def get_allocation_ip():
    return allocation_ip()


def get_allocation_index():
    return allocation_index()


def get_event():
    return hook_event()


def get_command():
    return hook_name()


def get_job():
    return job_name()


def kv_get(namespace, key):
    return get_store_value(namespace, key)


def kv_put(key, value):
    return put_job_variable(key, value)


def kv_put_secret(key, value):
    return put_job_secret(key, value)


def get_demands():
    return list_hook_demands()


def get_kv_value(namespace, key):
    """Read a KV value as a plaintext string."""
    response = get_store_value(namespace, key)
    response.raise_for_status()
    return response.json()["value"]


def find_bucket_root():
    """Locate the bucket root (directory containing kive.conf)."""
    for start in (Path.cwd().resolve(), Path(__file__).resolve().parent):
        for directory in [start, *start.parents]:
            if (directory / "kive.conf").is_file():
                return directory
    raise FileNotFoundError(f"kive.conf not found (cwd={Path.cwd()})")


# Short names inside grouped blocks → flat keys used by hooks / Go JSON.
_KIVE_BLOCK_KEYS = {
    "ssh": {
        "user": "ssh_user",
        "key": "ssh_key",
        "port": "ssh_port",
        "use_sudo": "use_sudo",
        "strict_host_key_checking": "strict_host_key_checking",
    },
    "log_config": {
        "format": "log_format",
        "run_retention_count": "log_run_retention_count",
        "run_retention_days": "log_run_retention_days",
    },
    "backup": {"retention_count": "backup_retention_count"},
    "health": {"wait_seconds": "health_wait_seconds"},
    "interpreters": {},  # names unchanged
    "job_signer": {"ca": "job_signer_ca", "ca_trust": "job_signer_ca_trust"},
}


def _parse_kive_conf(text):
    """Parse kive.conf blocks and legacy top-level calls (key=value fallback)."""
    conf = {}
    block = None
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line in ("};", "}"):
            block = None
            continue
        open_match = re.match(r"^(\w+)\s*\{\s*$", line)
        if open_match:
            block = open_match.group(1)
            continue
        match = re.match(r"^(\w+)\((.*)\)\s*;?\s*$", line)
        if match:
            key, raw = match.group(1), match.group(2).strip()
            if block is not None:
                mapping = _KIVE_BLOCK_KEYS.get(block)
                if mapping is not None:
                    key = mapping.get(key, key)
            if len(raw) >= 2 and raw[0] == raw[-1] and raw[0] in "'\"":
                conf[key] = raw[1:-1]
            else:
                conf[key] = raw
            continue
        if "=" in line:
            key, value = line.split("=", 1)
            conf[key.strip()] = value.strip().strip("'\"")
    return conf


def load_ssh():
    """Return (ssh_user, key_path, use_sudo) from kive.conf and secrets/."""
    bucket = find_bucket_root()
    conf = {}
    conf_path = bucket / "kive.conf"
    if conf_path.is_file():
        conf = _parse_kive_conf(conf_path.read_text())
    user = conf.get("ssh_user", "root")
    key_name = conf.get("ssh_key", "worker.key")
    use_sudo = conf.get("use_sudo", "false").lower() in ("true", "1", "yes")
    key = bucket / "secrets" / key_name
    if not key.is_file():
        fallback = bucket / key_name
        if fallback.is_file():
            key = fallback
        else:
            raise FileNotFoundError(
                f"SSH key not found: {key} (also checked {fallback})"
            )
    return user, key, use_sudo


def ssh_client_options(bucket, connect_timeout=15):
    """Return shared OpenSSH -o flags matching the Go worker SSH client."""
    conf = {}
    conf_path = bucket / "kive.conf"
    if conf_path.is_file():
        conf = _parse_kive_conf(conf_path.read_text())
    known_hosts = bucket / ".ssh" / "known_hosts"
    strict = conf.get("strict_host_key_checking", "yes").strip().lower() or "yes"
    port = conf.get("ssh_port", "22").strip() or "22"
    opts = [
        "-o",
        "BatchMode=yes",
        "-o",
        f"ConnectTimeout={connect_timeout}",
        "-o",
        f"StrictHostKeyChecking={strict}",
        "-o",
        f"UserKnownHostsFile={known_hosts}",
        "-o",
        "GlobalKnownHostsFile=/dev/null",
    ]
    if port != "22":
        return ["-p", port, *opts]
    return opts


def run_ssh(
    worker_ip,
    remote_cmd,
    *,
    timeout=300,
    check=True,
    connect_timeout=15,
):
    """Run a remote command over SSH using kive.conf worker credentials."""
    bucket = find_bucket_root()
    user, key_path, use_sudo = load_ssh()
    prefix = "sudo -E " if use_sudo else ""
    wrapped = f"{prefix}timeout {timeout} {remote_cmd}"
    return subprocess.run(
        [
            "ssh",
            "-i",
            str(key_path),
            *ssh_client_options(bucket, connect_timeout=connect_timeout),
            f"{user}@{worker_ip}",
            wrapped,
        ],
        check=check,
        text=True,
        capture_output=True,
        timeout=timeout + connect_timeout + 30,
    )


def run_runner_target(
    target,
    *,
    worker_ip=None,
    job=None,
    timeout=300,
):
    """Run runner.py <target> --jobs <job> on a worker (same as deploy)."""
    worker_ip = worker_ip or allocation_ip()
    job = job or job_name()
    bucket_id = get_kv_value("kive/bucket", "bucket_id")
    remote = (
        f"python3 {WORKER_INSTALL_ROOT}/{bucket_id}/bin/runner.py "
        f"{bucket_id} {target} --jobs {job}"
    )
    return run_ssh(worker_ip, remote, timeout=timeout)


def run_make_target(
    target,
    *,
    worker_ip=None,
    job=None,
    timeout=300,
):
    """Run make <target> in the job directory on a worker."""
    worker_ip = worker_ip or allocation_ip()
    job = job or job_name()
    bucket_id = get_kv_value("kive/bucket", "bucket_id")
    remote = f"make -C {WORKER_INSTALL_ROOT}/{bucket_id}/jobs/{job} {target}"
    return run_ssh(worker_ip, remote, timeout=timeout)
