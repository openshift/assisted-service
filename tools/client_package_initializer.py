import argparse
import os
import subprocess
import re
from typing import Dict


class Setup:
    NAME_KEY = "name"
    VERSION_KEY = "version"
    DESCRIPTION_KEY = "description"
    AUTHOR_KEY = "author"
    AUTHOR_EMAIL_KEY = "author_email"
    URL_KEY = "url"
    KEYWORDS_KEY = "keywords"
    INSTALL_REQUIRES_KEY = "install_requires"
    PACKAGES_KEY = "packages"
    INCLUDE_PACKAGE_DATA_KEY = "include_package_data"
    LONG_DESCRIPTION_KEY = "long_description"
    PYTHON_REQUIRES_KEY = "python_requires"

    FORMAT = """
import setuptools

setuptools.setup(
    name="{name}",
    description="{description}",
    author="{author}",
    author_email="{author_email}",
    version="{version}",
    url="{url}",
    keywords={keywords},
    install_requires={install_requires},
    packages={packages},
    include_package_data={include_package_data},    
    python_requires='>=3.6',
    long_description='''
    {long_description}''',
    long_description_content_type='text/markdown',
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
    VERSION_DIR_PATH = "assisted_service_client.egg-info"
    VERSION_PATH="PKG-INFO"
    DEFAULT_README_PATH = "README.md"

    def __init__(self, project_path: str, url: str) -> None:
        self._project_path = project_path
        self._setup_path = os.path.join(project_path, self.DEFAULT_SETUP_PATH)
        self._url = url
        self._version = None
        self._setup_data = {Setup.AUTHOR_KEY: "RedHat",
                            Setup.AUTHOR_EMAIL_KEY: "UNKNOWN",
                            Setup.URL_KEY: url
                            }

        with open(os.path.join(self._project_path, "MANIFEST.in"), "w") as f:
            f.write(f"include {self.DEFAULT_README_PATH}")

    def _load_readme(self) -> None:
        with open(os.path.join(self._project_path, "README.md")) as f:
            readme = f.read()

        self._setup_data[Setup.LONG_DESCRIPTION_KEY] = \
            readme.replace("git+https://github.com/GIT_USER_ID/GIT_REPO_ID.git", "assisted-service-client")

    def _load_setup(self) -> None:
        """
        Load setup.py arguments as python dict
        """
        data = ""

        with open(self._setup_path) as f:
            lines = f.readlines()

        for line in lines:
            if "setup(" in line:
                data += "SETUP = dict("
            else:
                data += line

        setup = self._execute(data)["SETUP"]
        for key in self._setup_data.keys():
            try:
                setup.pop(key)
            except KeyError:
                pass

        self._setup_data.update(setup)

    def load(self) -> "SetupInitializer":
        self._load_readme()
        self._load_setup()

        return self

    @classmethod
    def _execute(cls, data: str) -> Dict[str, Dict]:
        """
        Load dict from string into python dict
        :param data: Python dict as string
        :return: key value dict loaded from data
        """
        d = dict()
        exec(data, d)
        return d

    def dump(self) -> "SetupInitializer":
        """ Dump setup.py into file and build the python artifacts - wheel and tar.gz """

        with open(self._setup_path, "w") as f:
            f.write(Setup.FORMAT.format(**self._setup_data))

        self.build("bdist_wheel")

        with open(self._setup_path, "w") as f:
            f.write(Setup.FORMAT.format(**self._setup_data))

        subprocess.check_output(["python3", self._setup_path, "check"])

        with open(os.path.join(self._project_path, os.path.join(self.VERSION_DIR_PATH), self.VERSION_PATH), "r") as f:
            version: str = self._extract_api_version(text=f.read())
            commit_number = self._count_commits_from_head_to_tag(tag_name=f"v{version}")
            adjusted_version = f"{version.removeprefix('v')}.post{commit_number}"

        self._setup_data[Setup.VERSION_KEY] = "1.0.0.a39e8099885e26c5993a037f307367d16264e452"
    
        with open(self._setup_path, "w") as f:
            f.write(Setup.FORMAT.format(**self._setup_data))

        self.build("sdist")
        return self

    def build(self, distribution: str) -> None:
        """ Build python library """
        try:
            cmd = f"python3 {self._setup_path} {distribution} --dist-dir {os.path.join(self._project_path, 'dist/')}"
            out = subprocess.check_output(cmd.split())
            print(out.decode())
        except subprocess.CalledProcessError as e:
            print(e.output)

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description="Swagger package builder")
    parser.add_argument("base_path", help="Generated Python project directory path", type=str)
    parser.add_argument("url", help="Project url", type=str)
    args = parser.parse_args()

    SetupInitializer(args.base_path, args.url).load().dump()
