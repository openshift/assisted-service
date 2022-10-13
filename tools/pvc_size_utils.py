import subprocess
import utils
import shlex


class BytesSuffix(object):
    suffix_length = 0
    suffix_to_bytes = {'': 2 ** 0}

    @classmethod
    def has_suffix(cls, s):
        if cls.suffix_length > len(s):
            return False

        return bool(cls.suffix_to_bytes.get(s[-cls.suffix_length:]))

    @classmethod
    def to_bytes(cls, s):
        amount = cls.get_amount(s)
        if amount is None:
            raise ValueError(
                f'failed to convert size to bytes: {s}'
            )

        b = cls.suffix_to_bytes[cls.get_suffix(s)]
        return amount * b

    @classmethod
    def get_suffix(cls, s):
        return s[-cls.suffix_length:]

    @classmethod
    def get_amount(cls, s):
        if not s[:-cls.suffix_length].isdigit():
            return

        return int(s[:-cls.suffix_length])


class BinBytesSuffix(BytesSuffix):
    suffix_length = 2
    suffix_to_bytes = {
        'Ki': 2 ** 10,
        'Mi': 2 ** 20,
        'Gi': 2 ** 30,
        'Ti': 2 ** 40,
        'Pi': 2 ** 50,
        'Ei': 2 ** 60,
    }


class DecBytesSuffix(BytesSuffix):
    suffix_length = 1
    suffix_to_bytes = {
        'n': 10 ** -9,
        'u': 10 ** -6,
        'm': 10 ** -3,
        'k': 10 ** 3,
        'M': 10 ** 6,
        'G': 10 ** 9,
        'T': 10 ** 12,
        'P': 10 ** 15,
        'E': 10 ** 18
    }


def update_size_in_yaml_docs(target, ns, name, docs):
    req = extract_requested_size_from_yaml_docs(name, docs)
    cur = get_current_size_if_exist(target, ns, name)
    size = determine_which_size_to_deploy(req, cur)
    set_size_in_yaml_docs(name, size, docs)


def extract_requested_size_from_yaml_docs(name, docs):
    for d in docs:
        try:
            if name != d['metadata']['name'] or d['kind'] != 'PersistentVolumeClaim':
                continue
        except KeyError:
            continue

        return d['spec']['resources']['requests']['storage']

    raise ValueError(
        f'pvc: {name} was not found in yaml docs: {docs}'
    )


def get_current_size_if_exist(target, namespace, pvc_name):
    kubectl_cmd = utils.get_kubectl_command(target, namespace)

    jsonpath = "{.spec.resources.requests.storage}"

    command = shlex.split(kubectl_cmd) + [
        "get",
        "persistentvolumeclaims",
        pvc_name,
        f'-o=jsonpath="{jsonpath}"',
    ]

    process = subprocess.Popen(
        command,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )

    stderr = process.stderr.read().decode()
    stdout = process.stdout.read().decode()
    exit_code = process.wait()

    if "not found" in stderr:
        return None
    elif exit_code != 0:
        raise RuntimeError(
            f"failed to get size of pvc {pvc_name} exit {exit_code}: {stderr}"
        )
    elif stdout == "":
        raise RuntimeError(
            f"failed to get {jsonpath} of pvc {pvc_name}: empty output"
        )

    return stdout.strip('"')


def determine_which_size_to_deploy(req, cur=None):
    if cur is None:
        return req

    req_bytes = size_to_bytes(req)
    cur_bytes = size_to_bytes(cur)

    return req if req_bytes > cur_bytes else cur


def size_to_bytes(s):
    if BinBytesSuffix.has_suffix(s):
        return BinBytesSuffix.to_bytes(s)
    elif DecBytesSuffix.has_suffix(s):
        return DecBytesSuffix.to_bytes(s)
    return BytesSuffix.to_bytes(s)


def set_size_in_yaml_docs(name, size, docs):
    for d in docs:
        try:
            if name != d['metadata']['name'] or d['kind'] != 'PersistentVolumeClaim':
                continue
        except KeyError:
            continue

        d['spec']['resources']['requests']['storage'] = size
        return
