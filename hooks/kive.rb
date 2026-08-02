# frozen_string_literal: true

# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

# Client for the kive hook runtime API (KV store, demands, worker SSH).

require "json"
require "net/http"
require "open3"
require "pathname"
require "uri"

module Kive
  WORKER_INSTALL_ROOT = "/opt/kive"
  RUNTIME_API_DEFAULT_PORT = "8080"
  ROUTE_STORE_KEYS = "/kv"
  ROUTE_STORE_KEYS_LIST = "/kv/keys"
  ROUTE_STORE_SECRET = "/kv/secret"
  ROUTE_DEMANDS = "/demands"
  ROUTE_SEMAPHORE_ACQUIRE = "/semaphore/acquire"
  ROUTE_SEMAPHORE_RELEASE = "/semaphore/release"
  ROUTE_SEMAPHORE_STATUS = "/semaphore/status"
  ROUTE_HTTP = "/http"

  KIVE_BLOCK_KEYS = {
    "ssh" => {
      "user" => "ssh_user",
      "key" => "ssh_key",
      "port" => "ssh_port",
      "use_sudo" => "use_sudo",
      "strict_host_key_checking" => "strict_host_key_checking"
    },
    "log_config" => {
      "format" => "log_format",
      "run_retention_count" => "log_run_retention_count",
      "run_retention_days" => "log_run_retention_days"
    },
    "backup" => {"retention_count" => "backup_retention_count"},
    "health" => {"wait_seconds" => "health_wait_seconds"},
    "interpreters" => {},
    "job_signer" => {"ca" => "job_signer_ca", "ca_trust" => "job_signer_ca_trust"}
  }.freeze

  class << self
    def allocation_id
      ENV["ALLOCATION_ID"]
    end

    def allocation_ip
      ENV["ALLOCATION_IP"]
    end

    def allocation_index
      ENV["ALLOCATION_INDEX"]
    end

    def allocation_disabled?
      ENV["DISABLED"] == "1"
    end

    # True on the first allocation of the first batch.
    # Prefer BATCH_ALLOCATIONS[0] over ALLOCATION_INDEX=0: rollout order can put
    # catalog index 0 in a later batch, which would otherwise make no worker one-shot.
    def one_shot?
      return false unless (ENV["BATCH_INDEX"] || "0") == "0"

      batch = (ENV["BATCH_ALLOCATIONS"] || "").split(",").map(&:strip).reject(&:empty?)
      return (ENV["ALLOCATION_IP"] || "").strip == batch.first if batch.any?

      (ENV["ALLOCATION_INDEX"] || "0") == "0"
    end

    def hook_event
      ENV["EVENT"]
    end

    def hook_name
      ENV["HOOK"]
    end

    def job_name
      ENV["JOB"]
    end

    def runtime_api_base_url
      host = ENV["HOOK_API_HOST"] || "127.0.0.1"
      port = ENV["HOOK_API_PORT"] || RUNTIME_API_DEFAULT_PORT
      "http://#{host}:#{port}"
    end

    def runtime_request_headers
      headers = {
        "X-ALLOCATION-ID" => allocation_id.to_s,
        "HOOK" => hook_name.to_s,
        "EVENT" => hook_event.to_s,
        "Content-Type" => "application/json",
        "Accept" => "application/json",
      }
      token = ENV["HOOK_API_TOKEN"]
      headers["Authorization"] = "Bearer #{token}" if token && !token.empty?
      headers
    end

    def runtime_api_request(method, path, body: nil, params: nil, timeout: nil)
      uri = URI("#{runtime_api_base_url}#{path}")
      uri.query = URI.encode_www_form(params) if params
      http = Net::HTTP.new(uri.host, uri.port)
      http.open_timeout = 15
      http.read_timeout = timeout || 120
      req =
        case method
        when :get then Net::HTTP::Get.new(uri)
        when :put then Net::HTTP::Put.new(uri)
        when :post then Net::HTTP::Post.new(uri)
        when :delete then Net::HTTP::Delete.new(uri)
        else
          raise ArgumentError, "unsupported method #{method}"
        end
      runtime_request_headers.each { |k, v| req[k] = v }
      req.body = JSON.generate(body) unless body.nil?
      http.request(req)
    end

    # Legacy alias — prefer runtime_api_request for loopback API calls.
    def http_request_runtime(method, path, body: nil, params: nil, timeout: nil)
      runtime_api_request(method, path, body: body, params: params, timeout: timeout)
    end

    def get_store_value(namespace, key)
      runtime_api_request(:get, ROUTE_STORE_KEYS, body: { "namespace" => namespace, "key" => key })
    end

    def put_rollout_order(order)
      value =
        if order.is_a?(Array)
          order.map(&:to_s).map(&:strip).reject(&:empty?).join(",")
        else
          order
        end
      runtime_api_request(
        :put,
        ROUTE_STORE_KEYS,
        body: {
          "namespace" => "kive/job/#{job_name}",
          "key" => "rollout_order",
          "value" => value,
        },
      )
    end

    def get_rollout_order
      get_store_value("kive/job/#{job_name}", "rollout_order")
    end

    def put_job_variable(key, value, ttl: 0)
      body = {
        "namespace" => "vars/job/#{job_name}",
        "key" => key,
        "value" => value,
      }
      body["ttl"] = ttl if ttl && ttl != 0
      runtime_api_request(:put, ROUTE_STORE_KEYS, body: body)
    end

    def put_job_secret(key, value, ttl: 0)
      body = {
        "namespace" => "secrets/job/#{job_name}",
        "key" => key,
        "value" => value,
      }
      body["ttl"] = ttl if ttl && ttl != 0
      runtime_api_request(:put, ROUTE_STORE_SECRET, body: body)
    end

    def list_job_keys(namespace = nil)
      body = {}
      body["namespace"] = namespace unless namespace.nil?
      runtime_api_request(:get, ROUTE_STORE_KEYS_LIST, body: body)
    end

    def delete_job_variable(key)
      runtime_api_request(
        :delete,
        ROUTE_STORE_KEYS,
        body: {
          "namespace" => "vars/job/#{job_name}",
          "key" => key,
        },
      )
    end

    def delete_job_secret(key)
      runtime_api_request(
        :delete,
        ROUTE_STORE_SECRET,
        body: {
          "namespace" => "secrets/job/#{job_name}",
          "key" => key,
        },
      )
    end

    def list_hook_demands
      runtime_api_request(:get, ROUTE_DEMANDS)
    end

    def acquire_semaphore(name, capacity: 1, timeout_seconds: 600)
      runtime_api_request(
        :post,
        ROUTE_SEMAPHORE_ACQUIRE,
        body: {
          "name" => name,
          "capacity" => capacity,
          "timeout_seconds" => timeout_seconds,
        },
        timeout: timeout_seconds + 30,
      )
    end

    def release_semaphore(name)
      runtime_api_request(:post, ROUTE_SEMAPHORE_RELEASE, body: { "name" => name })
    end

    def semaphore_status(name)
      runtime_api_request(:get, ROUTE_SEMAPHORE_STATUS, params: { "name" => name })
    end

    # Outbound HTTP(S) via POST /http (worker-IP allowlist + bucket CA).
    def http_request(method, url, headers: {}, body: "", timeout_seconds: 30, tls: {})
      resp = runtime_api_request(
        :post,
        ROUTE_HTTP,
        body: {
          "method" => method,
          "url" => url,
          "headers" => headers || {},
          "body" => body || "",
          "timeout_seconds" => timeout_seconds,
          "tls" => tls || {},
        },
        timeout: timeout_seconds + 30,
      )
      raise "http_request failed: #{resp.code} #{resp.body}" if resp.code.to_i >= 400

      JSON.parse(resp.body)
    end

    def get_kv_value(namespace, key)
      response = get_store_value(namespace, key)
      raise "KV get failed: #{response.code} #{response.body}" unless response.is_a?(Net::HTTPSuccess)

      JSON.parse(response.body).fetch("value")
    end

    def find_bucket_root
      starts = [Pathname.pwd.expand_path, Pathname.new(__FILE__).expand_path.dirname]
      starts.each do |start|
        [start, *start.ascend.to_a].each do |directory|
          return directory if (directory / "kive.conf").file?
        end
      end
      raise Errno::ENOENT, "kive.conf not found (cwd=#{Dir.pwd})"
    end

    def parse_kive_conf(text)
      conf = {}
      block = nil
      text.each_line do |line|
        line = line.strip
        next if line.empty? || line.start_with?("#")

        if ["}", "};"].include?(line)
          block = nil
          next
        end
        if (open_match = line.match(/\A(\w+)\s*\{\s*\z/))
          block = open_match[1]
          next
        end

        if (match = line.match(/\A(\w+)\((.*)\)\s*;?\s*\z/))
          key = match[1]
          raw = match[2].strip
          if !block.nil? && KIVE_BLOCK_KEYS.key?(block)
            key = KIVE_BLOCK_KEYS[block].fetch(key, key)
          end
          conf[key] = if raw.length >= 2 && raw[0] == raw[-1] && %w[' "].include?(raw[0])
                        raw[1..-2]
                      else
                        raw
                      end
          next
        end
        next unless line.include?("=")

        key, value = line.split("=", 2)
        conf[key.strip] = value.strip.delete_prefix("'").delete_suffix("'").delete_prefix('"').delete_suffix('"')
      end
      conf
    end

    def load_ssh
      bucket = find_bucket_root
      conf = {}
      conf_path = bucket / "kive.conf"
      conf = parse_kive_conf(conf_path.read) if conf_path.file?
      user = conf.fetch("ssh_user", "root")
      key_name = conf.fetch("ssh_key", "worker.key")
      use_sudo = %w[true 1 yes].include?(conf.fetch("use_sudo", "false").downcase)
      key = bucket / "secrets" / key_name
      unless key.file?
        fallback = bucket / key_name
        raise Errno::ENOENT, "SSH key not found: #{key} (also checked #{fallback})" unless fallback.file?

        key = fallback
      end
      [user, key, use_sudo]
    end

    def ssh_client_options(bucket, connect_timeout: 15)
      conf = {}
      conf_path = bucket / "kive.conf"
      conf = parse_kive_conf(conf_path.read) if conf_path.file?
      known_hosts = bucket / ".ssh" / "known_hosts"
      strict = conf.fetch("strict_host_key_checking", "yes").strip.downcase
      strict = "yes" if strict.empty?
      port = conf.fetch("ssh_port", "22").strip
      port = "22" if port.empty?
      opts = [
        "-o", "BatchMode=yes",
        "-o", "ConnectTimeout=#{connect_timeout}",
        "-o", "StrictHostKeyChecking=#{strict}",
        "-o", "UserKnownHostsFile=#{known_hosts}",
        "-o", "GlobalKnownHostsFile=/dev/null",
      ]
      port == "22" ? opts : ["-p", port, *opts]
    end

    def run_ssh(worker_ip, remote_cmd, timeout: 300, check: true, connect_timeout: 15)
      bucket = find_bucket_root
      user, key_path, use_sudo = load_ssh
      prefix = use_sudo ? "sudo -E " : ""
      wrapped = "#{prefix}timeout #{timeout} #{remote_cmd}"
      cmd = [
        "ssh",
        "-i", key_path.to_s,
        *ssh_client_options(bucket, connect_timeout: connect_timeout),
        "#{user}@#{worker_ip}",
        wrapped,
      ]
      stdout, stderr, status = Open3.capture3(*cmd)
      raise "ssh failed: #{stderr}" if check && !status.success?

      { stdout: stdout, stderr: stderr, status: status, success: status.success? }
    end

    def run_runner_target(target, worker_ip: nil, job: nil, timeout: 300)
      worker_ip ||= allocation_ip
      job ||= job_name
      bucket_id = get_kv_value("kive/bucket", "bucket_id")
      remote = "python3 #{WORKER_INSTALL_ROOT}/#{bucket_id}/bin/runner.py #{bucket_id} #{target} --jobs #{job}"
      run_ssh(worker_ip, remote, timeout: timeout)
    end

    def run_make_target(target, worker_ip: nil, job: nil, timeout: 300)
      worker_ip ||= allocation_ip
      job ||= job_name
      bucket_id = get_kv_value("kive/bucket", "bucket_id")
      remote = "make -C #{WORKER_INSTALL_ROOT}/#{bucket_id}/jobs/#{job} #{target}"
      run_ssh(worker_ip, remote, timeout: timeout)
    end

    alias get_allocation_id allocation_id
    alias get_allocation_ip allocation_ip
    alias get_allocation_index allocation_index
    alias get_event hook_event
    alias get_command hook_name
    alias get_job job_name
    alias is_allocation_disabled allocation_disabled?
    alias is_one_shot one_shot?
    alias kv_get get_store_value
    alias kv_put put_job_variable
    alias kv_put_secret put_job_secret
    alias get_demands list_hook_demands
  end
end
