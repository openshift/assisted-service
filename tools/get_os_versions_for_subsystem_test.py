import os
import json
from functools import cmp_to_key

def compareVersionNumber(item1, item2):
	v1Split = item1.split(".")
	v2Split = item2.split(".")
	v1Len = len(v1Split)
	v2Len = len(v2Split)
	maxParts = max(v1Len, v2Len)
	for partIndex in range(maxParts):
		v1VersionValue = 0
		v2VersionValue = 0
		if partIndex +1 <= v1Len:
			v1VersionValue = int(v1Split[partIndex])
		if partIndex +1 <= v2Len:
			v2VersionValue = int(v2Split[partIndex])
		if v1VersionValue > v2VersionValue:
			return 1
		if v1VersionValue < v2VersionValue:
			return -1
	return 0


def getHighestVersion(ENTRIES, versionKey):
	versions = []
	for ENTRY in ENTRIES:
		versions.append(ENTRY[versionKey])
	versions.sort(reverse=True, key=cmp_to_key(compareVersionNumber))
	return versions[0]

	
def appendImagesForVersion(ENTRIES, VERSION_NUMBER, versionKey, versions):
	for ENTRY in ENTRIES:
		if ENTRY[versionKey] == VERSION_NUMBER:  #and (ENTRY['cpu_architecture'] == 'x86_64' or ENTRY['cpu_architecture'] == 'multi'):
			versions.append(ENTRY)
	return versions


DEFAULT_OS_IMAGES_FILE = os.path.dirname(__file__) + "/../data/default_os_images.json"
f = open(DEFAULT_OS_IMAGES_FILE)
OS_IMAGES = json.load(f)
OPENSHIFT_VERSION = getHighestVersion(OS_IMAGES, 'openshift_version')
versions = []
versions = appendImagesForVersion(OS_IMAGES, OPENSHIFT_VERSION, 'openshift_version', versions)
versions = appendImagesForVersion(OS_IMAGES, "4.12", 'openshift_version', versions)
versions = appendImagesForVersion(OS_IMAGES, "4.13", 'openshift_version', versions)
print(json.dumps(versions))
