# Installer Cache for Openshift Releases
During the phase where a cluster is prepared for installation, the manifests for the cluster need to be generated.
In order to generate manifests, the installation binary for the desired version of Openshift is required. 
It is the job of the Installer Cache to provide this binary.

The installer cache can be operated in a mode where it will function as a 'Least Recently Used' (LRU cache) or as a simple directory of releases. 

## Settings
The installer cache has a number of settings, which can be overridden by providing them in as environment variables in the assisted service config map.

### INSTALLER_CACHE_CAPACITY

This defaults to `0`, which means that there is no limit to the size of the installer cache.
This should be kept at zero if the storage directory is not ephemeral.

Otherwise, it will accept a capacity, followed by either "GiB", "MiB", "KiB" or "B" 

For example "32GiB"

### INSTALLER_CACHE_MAX_RELEASE_SIZE

This is the estimated maximum size that we believe any Openshift release could ever be. The default at the time of writing is 2GiB.
This setting is used by the installercache to decide if there is enough space to write a release to the cache or if some things will need to be evicted first.

This will accept a capacity, followed by either "GiB", "MiB", "KiB" or "B" 
For example "1GiB"

### INSTALLER_CACHE_RELEASE_FETCH_RETRY_INTERVAL

If the cache finds itself unable to write a release (as indicated by `INSTALLER_CACHE_MAX_RELEASE_SIZE`) to the cache without breaking the cache limit
then it is possible that a request to fetch a release may need to be retried.
`INSTALLER_CACHE_RELEASE_FETCH_RETRY_INTERVAL` is the interval at which this retry should be attempted.
This is expressed as a duration, for example "30s"

## Where the files are stored

The files will be stored on the volume that is mapped to the working directory of the pod, defined as `WORK_DIR` in environment variables.
There is one instance of the installer cache per node. This means that in SAAS for example, there are three independent caches, one for each node.
This is entirely expected and normal.

## Usage

Usage of the cache is quite straightforward...

The caller of the installer cache makes a call to 

```
func (i *Installers)  Get(ctx context.Context, releaseID, releaseIDMirror, pullSecret string, ocRelease oc.Release, ocpVersion string, clusterID strfmt.UUID) (*Release, error)
```

This call returns an `*installercache.Release` instance that the caller must retain until they are finished using the release. Once the caller is done, they call `release.Cleanup(...)` which advises the cache that the user is done with the release.

The cache guarantees to never breach `INSTALLER_CACHE_CAPACITY`.



