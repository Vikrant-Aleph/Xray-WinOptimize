# Xray-WinOptimize (Experimental)

An **unofficial experimental Windows TCP outbound backend** for Xray-core. It aims to use Xray's in-tree lightweight TCP/IP stack, WinDivert packet I/O and a Linux-inspired receive-window growth controller, rather than relying on native Windows TCP for Xray's ordinary outbound TCP dials.

**Not production-ready. Full Xray compilation, Windows packet-driver interoperability and end-to-end connectivity have not yet been verified. Performance improvement is not established.**

It is intended for **TCP outbound connections using Xray's default dialer**; existing SOCKS/HTTP proxy inbounds remain unchanged. It does not replace TCP for other Windows applications, nor does it modify UDP. Some outbound options and network paths are unsupported.

## Install an experimental build (Windows x64)

1. Open this repository's **Actions** and download an artifact from a successful `Track upstream Xray release and build experimental TCP backend` run. **Releases** are only published if the repository owner explicitly enables experimental prereleases. Keep your official Xray executable for rollback.
2. Extract `xray-winrecv-experimental.exe`. From the [official WinDivert v2.2.2 release](https://github.com/basil00/WinDivert/releases/tag/v2.2.2), obtain the matching x64 `WinDivert.dll` and signed `WinDivert64.sys`, and place them next to the executable. **The driver and DLL are not bundled.**
3. Use your existing Xray config, remove any test `SO_RCVBUF` custom socket option, and start from an **Administrator PowerShell** (adjust config path):

   ```powershell
   $env:XRAY_EXPERIMENTAL_TCP_STACK = '1'
   .\xray-winrecv-experimental.exe run -config .\config.json
   ```

4. Verify single-connection TCP and REALITY connectivity **before** attempting a speed test. This experimental packet driver may conflict with VPNs, firewalls and other network filters.

**Rollback:** stop experimental Xray, then run your saved official binary:

```powershell
Remove-Item Env:\XRAY_EXPERIMENTAL_TCP_STACK -ErrorAction SilentlyContinue
.\xray.exe run -config .\config.json
```

See [detailed Windows installation / source build](docs/INSTALL-WINDOWS.md).

## Development and documentation

The [technical design and change log](docs/TECHNICAL.md) documents architecture, exact source modifications, algorithm, packet adapter, compatibility risks, test status and handoff notes. **Every implementation, patch, workflow or installation-process change must update that document in the same pull request**; a CI documentation gate enforces the file-change requirement.

[简体中文](README.md) · [Upstream Xray-core](https://github.com/XTLS/Xray-core) (this is an independent, unofficial project)
