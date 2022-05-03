# Porto

Tool for adding [vanity imports](https://sagikazarmark.hu/blog/vanity-import-paths-in-go/) URI to Go files.

e.g. `package zipkin` -> `package zipkin // import "github.com/openzipkin/zipkin-go"`

## Install

```bash
go install github.com/jcchavezs/porto/cmd/porto
```

## Getting started

Run the tool and display the changes without applying them

```bash
porto path/to/library
```

If you want the changes to be applied to the files directly, run:

```bash
porto -w path/to/library
```

If you just want to list the files that porto would change vanity import, run:

```bash
porto -l path/to/library
```

If you want to ignore files (e.g. proto generated files), pass the `--skip-files` flag:

```bash
porto --skip-files ".*\\.pb\\.go$" path/to/library
```
