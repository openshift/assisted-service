import argparse
import os
import subprocess
from typing import Dict, Tuple


class Setup:
    NAME_KEY = "name"
    DESCRIPTION_KEY = "description"
    SETUP_REQUIRES_KEY = "setup_requires"
    VCVERSIONER_KEY = "vcversioner"
    AUTHOR_KEY = "author"
    AUTHOR_EMAIL_KEY = "author_email"
    URL_KEY = "url"
    KEYWORDS_KEY = "keywords"
    INSTALL_REQUIRES_KEY = "install_requires"
    PACKAGES_KEY = "packages"
    INCLUDE_PACKAGE_DATA_KEY = "include_package_data"
    LONG_DESCRIPTION_KEY = "long_description"
    PYTHON_REQUIRES_KEY = "python_requires"
    all = [NAME_KEY, DESCRIPTION_KEY, SETUP_REQUIRES_KEY, VCVERSIONER_KEY, AUTHOR_KEY, AUTHOR_EMAIL_KEY, URL_KEY,
           KEYWORDS_KEY, INSTALL_REQUIRES_KEY, PACKAGES_KEY, INCLUDE_PACKAGE_DATA_KEY, LONG_DESCRIPTION_KEY,
           PYTHON_REQUIRES_KEY]

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
    python_requires='>=3.6',
    long_description='''
    {long_description}''',
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


class SetupInitializer:
    DEFAULT_SETUP_PATH = "setup.py"
    DEFAULT_VERSION_PATH = "version.txt"
    DEFAULT_README_PATH = "README.md"

    def __init__(self, project_path: str, url: str) -> None:
        self._project_path = project_path
        self._setup_path = os.path.join(project_path, self.DEFAULT_SETUP_PATH)
        self._url = url
        self._version = None
        self._setup_data = {"setup_requires": ['vcversioner'],
                            "vcversioner": {'vcs_args': ['git', 'describe', '--tags', '--long']}}
        self._readme = None
        self._tests_require = ""

    def _load_readme(self) -> None:
        """
        Load readme file for setup.py long description
        :return:
        """
        try:
            with open(os.path.join(self._project_path, self.DEFAULT_README_PATH)) as f:
                self._readme = f.read()
        except FileNotFoundError:
            self._readme = ""

    def _load_setup(self) -> None:
        """
        Load setup.py arguments as python dict
        """
        data = ""

        with open(self._setup_path) as f:
            lines = f.readlines()

        for line in lines:
            if "setup(" in line:
                data += "SETUP = dict(\n"
            else:
                data += line

        self._setup_data.update(self._execute(data)["SETUP"])
        self._setup_data['author'] = "redhat"
        self._setup_data['author_email'] = "UNKNOWN"
        self._setup_data['url'] = self._url

    def load(self) -> "SetupInitializer":
        self._load_readme()
        self._load_setup()

        return self

    @classmethod
    def _execute(cls, data: str) -> Dict[str, object]:
        """
        Load dict from string into python dict
        :param data: Python dict as string
        :return: key value dict loaded from data
        """
        d = dict()
        exec(data, d)
        return d

    def dump(self) -> "SetupInitializer":
        """
        Dump setup.py into file
        :return:
        """
        if "version" in self._setup_data:
            self._setup_data.pop("version")
        if self._readme:
            self._setup_data["long_description"] = self._readme

        with open(self._setup_path, "w") as f:
            f.write(Setup.FORMAT.format(**self._setup_data))
        return self

    @classmethod
    def __system_execute(cls, cmd: str) -> Tuple[str, str]:
        """
        Execute system command
        :param cmd: Command to execute
        :return: stdout, error
        """
        process = subprocess.Popen(cmd.split(), stdout=subprocess.PIPE)
        output, error = process.communicate()
        return output, error

    def build(self) -> None:
        """ Build python library """
        self.__system_execute("pip install wheel vcversioner ")
        self.__system_execute(f"python {self._setup_path} bdist_wheel --dist-dir=.assisted-service-client")


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description="Swagger package builder")
    parser.add_argument("base_path", help="Generated Python project directory path", type=str)
    parser.add_argument("url", help="Project url", type=str)
    parser.add_argument("--build", help="If exists, generate wheel artifact", action="store_true")
    args = parser.parse_args()
    _base_path = args.base_path
    _url = args.url
    _build = args.build

    pkg = SetupInitializer(_base_path, _url).load().dump()
    if _build:
        pkg.build()
