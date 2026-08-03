# Copyright 2014 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

import json
import sys

WORKER_INSTALL_ROOT = "/opt/kive"

def main(json_file, bucket_id, worker_id, generation):
    try:
        with open(json_file, 'r') as file:
            data = json.load(file)

        if data.get("bucket_id") != bucket_id:
           sys.exit(1)
        if data.get("worker_id") != worker_id:
            sys.exit(2)
        if data.get("generation") != generation:
            sys.exit(3)

        sys.exit(0)
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(2)

if __name__ == "__main__":
    if len(sys.argv) != 4:
        print("Usage: python worker.py <bucket_id> <worker_id> <generation>")
        sys.exit(3)

    bucket_id = sys.argv[1]
    worker_id = sys.argv[2]
    generation = int(sys.argv[3])  # Ensure generation is treated as an integer
    json_file = f"{WORKER_INSTALL_ROOT}/{bucket_id}/worker.json"

    main(json_file, bucket_id, worker_id, generation)
