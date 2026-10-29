# Xray-WinOptimize

An **unofficial, experimental Windows performance fork of Xray-core**. Its default **Portable** mode uses Windows Winsock and a Linux-*inspired* receiver-buffer growth heuristic to help high-RTT/high-congestion traffic, without administrator privileges or an external driver.

This is **not** a port of Linux TCP. It monitors RTT, incoming byte counts, and window/buffer state via `SIO_TCP_INFO`, then selectively uses `SO_RCVBUF`. Explicit receive-buffer settings alter Windows autotuning behavior. Full upstream Xray builds and real-world throughput remain to be validated.

### Download & run

1. Open **Actions → Xray-WinOptimize Portable (latest upstream)** and run the workflow. Leave the version blank to track the latest published upstream Xray release, including prereleases.
2. Download the successful artifact, unpack `xray-winopt-portable.exe`, and remove any earlier manual `SO_RCVBUF` custom socket option from your Xray configuration.
3. Launch as a **normal user**, just like official Xray:

   ```powershell
   .\xray-winopt-portable.exe run -config .\config.json
   ```

Disable the optimization with `$env:XRAY_WINOPT_MODE='off'`. Set `$env:XRAY_WINOPT_LOG='1'` for diagnostic messages. The per-connection cap and overall buffer-request budget scale with system RAM; override them with `XRAY_WINOPT_MAX_MIB=1..64` and `XRAY_WINOPT_BUDGET_MIB=32..2048` (MiB).

The optional separate WinDivert-based TCP backend is an **experimental, privileged** research branch, not part of the default Portable executable.

Detailed architecture/change log: [docs/TECHNICAL.md](docs/TECHNICAL.md). Windows notes: [docs/INSTALL-WINDOWS.md](docs/INSTALL-WINDOWS.md). Upstream: [XTLS/Xray-core](https://github.com/XTLS/Xray-core).
