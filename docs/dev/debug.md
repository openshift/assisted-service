## Debugging

To debug assisted-service, run:

```bash
make deploy-service-debug
```

It will deploy assisted-service in a debug mode together with its components. To connect to the dlv session, you need to connect to assisted-service on port `40000`. For example, in vscode this configuration should do the trick: (after installing go extension and dlv)

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Remote - debug",
            "type": "go",
            "debugAdapter": "dlv-dap",
            "request": "attach",
            "mode": "remote",
            "remotePath": "",
            "port": 40000,
            "host": "127.0.0.1"
        },
    ]
}
```

if you wnat to debug remote service, change the `"host"` value the the `IP` of the host it is running on.

