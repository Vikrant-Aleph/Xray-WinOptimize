# Xray-WinOptimize

An unofficial Windows performance optimization project for Xray-core. It draws on Linux TCP receive-buffer autotuning design principles to implement its own adaptive receive-buffer growth policy for Windows TCP outbound connections.

The default build uses Windows native TCP without requiring administrator privileges or extra network drivers. It does **not** port the Linux TCP stack, replicate the Linux kernel autotuning algorithm, or guarantee a throughput gain in all environments. Experimental status.

### Download and run

1. Open **Actions → Xray-WinOptimize (latest upstream)** and manually run the workflow. Leave the version blank to use the latest Xray upstream Release, including prereleases.
2. Download the successful Artifact, unpack `xray-winopt.exe` and remove any manually configured `SO_RCVBUF` from your Xray configuration.
3. Run normally:

   ```powershell
   .\xray-winopt.exe run -config .\config.json
   ```

By default, short-lived path RTT observations may be reused *after* a connection is established, but the previous connection's receive-buffer size is not applied before SYN. For an explicit legacy performance comparison only, set `XRAY_WINOPT_PRECONNECT_CACHE=on` before launch (this may alter TCP handshake window characteristics). To disable optimization, set `XRAY_WINOPT_MODE=off` before launch. For connection-level TCP timestamp experimentation, set `XRAY_WINOPT_TCP_TIMESTAMPS=on` (default: do not modify timestamps). Enable logging with `XRAY_WINOPT_LOG=1`. Timestamp changes are **not** proven to improve speed.

See [technical design and change log](docs/TECHNICAL.md), [Windows setup](docs/INSTALL-WINDOWS.md), and [publishing](docs/PUBLISH-GITHUB.md). The separate WinDivert TCP backend remains an optional research path and is not part of the standard executable.
