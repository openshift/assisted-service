import argparse
import os
import subprocess


class SetupInitializer:
    DEFAULT_SETUP_PATH = "setup.py"
    DEFAULT_VERSION_PATH = "version.txt"
    ASSISTED_URL = "https://github.com/openshift/assisted-service"

    FORMAT = """
import setuptools

setuptools.setup(
    name="{name}",
    description="{description}",
    setup_requires={setup_requires},
    vcversioner={vcversioner},
    author="{author}",
    author_email="{author_email}",
    url="{url}",
    keywords={keywords},
    install_requires={install_requires},
    packages={packages},
    include_package_data={include_package_data},
    long_description='''
    {long_description}''',
    python_requires='>=3.6',
    classifiers=[
        'Development Status :: 3 - Alpha',
        'Intended Audience :: Developers',
        'Intended Audience :: Information Technology',
        'License :: OSI Approved :: Apache Software License',
        'Programming Language :: Python :: 3',
        'Programming Language :: Python :: 3.6',
        'Programming Language :: Python :: 3.7',
        'Programming Language :: Python :: 3.8',
        'Programming Language :: Python :: 3.9',
    ]
)
"""

    def __init__(self, project_path: str) -> None:
        self._setup_path = os.path.join(project_path, self.DEFAULT_SETUP_PATH)
        self._version = None
        self._setup_data = {"setup_requires": ['vcversioner'],
                            "vcversioner": {'vcs_args': ['git', 'describe', '--tags', '--long']}}

    def load(self) -> "SetupInitializer":
        data = ""

        with open(self._setup_path) as f:
            lines = f.readlines()

        for line in lines:
            if "setup(" in line:
                data += "SETUP = dict(\n"
            else:
                data += line

        self._execute(data)
        self._setup_data['author'] = "redhat"
        self._setup_data['author_email'] = "UNKNOWN"
        self._setup_data['url'] = self.ASSISTED_URL

        return self

    def _execute(self, data: str) -> None:
        d = dict()
        exec(data, d)
        self._setup_data.update(d["SETUP"])

    def dump(self) -> "SetupInitializer":
        if "version" in self._setup_data:
            self._setup_data.pop("version")
        with open(self._setup_path, "w") as f:
            f.write(self.FORMAT.format(**self._setup_data))
        return self

    @classmethod
    def __system_execute(cls, cmd: str) -> (str, str):
        process = subprocess.Popen(cmd.split(), stdout=subprocess.PIPE)
        output, error = process.communicate()
        return output, error

    def build(self) -> None:
        self.__system_execute("pip install wheel")
        self.__system_execute(f"python {self._setup_path} bdist_wheel --dist-dir=.assisted-service-client")


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description="Assisted client package builder")
    parser.add_argument("base_path", help="Assisted service client project directory path", type=str)
    parser.add_argument("--build", help="If exists, generate wheel artifact", action="store_true")
    args = parser.parse_args()
    base_path = args.base_path
    build = args.build

    pkg = SetupInitializer(base_path).load().dump()
    if build:
        pkg.build()
