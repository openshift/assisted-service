#!/usr/bin/env python3
import datetime

import utils
from retry import retry

TRIES = 10
DELAY = 5


@retry(exceptions=RuntimeError, tries=TRIES, delay=DELAY)
def is_cluster_info_ready(kubectl_cmd):
    cmd = f"{kubectl_cmd} cluster-info"
    print(f"{datetime.datetime.now()} DEBUG - {cmd}")
    try:
        res = utils.check_output(cmd)
    except RuntimeError:
        print(f"{datetime.datetime.now()} DEBUG - cluster is not ready yet.")
        raise
    print(res)
    return


def main():
    is_cluster_info_ready(kubectl_cmd=utils.get_kubectl_command())


if __name__ == '__main__':
    main()
