# Xray-WinOptimize

[简体中文](README.md) | English

On **my own Windows PC, network, and VPS setup**, I noticed that **when the network became congested and latency was high, Windows TCP receive-window autotuning continued to operate but did not grow the receive buffer quickly enough, and Xray download speeds dropped noticeably**. Increasing `SO_RCVBUF` manually brought a substantial improvement. **That observation is what started Xray-WinOptimize.**

Xray-WinOptimize is a Windows TCP outbound performance optimization project built on Xray-core. Drawing on Linux TCP receive-buffer autotuning principles, it uses the **native Windows TCP stack and socket interfaces** to adjust the receive buffer based on connection conditions and improve download throughput on congested, high-latency paths.

## Background and test results

During the investigation, Windows TCP connection statistics showed that the default receive buffer did grow over time, but under congestion it could grow too slowly to keep up with download demand. Explicitly setting a larger `SO_RCVBUF` made sufficient receive-window capacity available sooner.

In my network and VPS test environment, download throughput with the modified Xray-core **increased from approximately 40 Mbps to 70 Mbps**. This project turns that observation into a connection-aware approach that reduces the need to configure a fixed receive-buffer size manually.

## How it works

- **Xray outbound connections:** Targets Windows outbound TCP connections created through Xray's default TCP dialer.
- **Adaptive receive buffering:** Adjusts the receive buffer according to connection conditions through Windows socket interfaces, including `SO_RCVBUF`.
- **Native operation:** Uses the Windows TCP stack and normally requires no administrator privileges, additional network drivers, or system-wide TCP changes.
- **Easy to disable:** Set `XRAY_WINOPT_MODE=off` to disable the optimization for comparisons or rollback.

For algorithm details, configuration options, and source implementation, see the [technical documentation](docs/TECHNICAL.md).

## Downloads and automatic releases

Download the build for your platform from [GitHub Releases](https://github.com/Vikrant-Aleph/Xray-WinOptimize/releases):

| Release archive | Target platform |
| --- | --- |
| `Xray-winopt-win7-32.zip` | Windows 7 x86 |
| `Xray-winopt-win7-64.zip` | Windows 7 x64 |
| `Xray-winopt-win-32.zip` | Modern Windows x86 |
| `Xray-winopt-win-64.zip` | Modern Windows x64 |
| `Xray-winopt-win-arm-64.zip` | Windows ARM64 |

**Each ZIP contains only `xray.exe`, `LICENSE`, and `BUILD-INFO.txt`.** The executable has a consistent `xray.exe` name and can be used with compatible command-line setups and graphical clients.

Every **other day**, GitHub Actions checks the latest published Xray-core Release (including prereleases). If that upstream version does not yet have a complete WinOptimize Release, the workflow builds all five targets. **A new Release is published after all five builds succeed and pass architecture checks.** You can also run `Xray-WinOptimize (latest upstream)` manually from [Actions](https://github.com/Vikrant-Aleph/Xray-WinOptimize/actions); leave the version input empty to use the latest upstream Release.

**Windows 7 compatibility:** These builds use the [XTLS/go-win7](https://github.com/XTLS/go-win7) toolchain. The adaptive optimization depends on `SIO_TCP_INFO`, which may be unavailable on Windows 7, so runtime compatibility and the optimization's effectiveness need to be verified on a real machine.

## Installation and usage

Back up the existing `xray.exe` and configuration first. If you manually set an outbound `SO_RCVBUF` option, remove it before evaluating the adaptive behavior.

### Command line

Put the release's `xray.exe` in your existing Xray runtime directory and start it with the current configuration:

```powershell
.\xray.exe run -config .\config.json
```

### Replace the core in a graphical client (such as v2rayN)

1. Fully exit the client, stop the Xray process, and back up the original `xray.exe`.
2. Locate the Xray core directory used by the client. A common v2rayN location on Windows is `bin\xray\`; check your actual installation path.
3. Copy the release's `xray.exe` into that directory, replacing the original core. Keep the GUI executable and other files intact.
4. Restart the client, select the Xray core, and check its logs for successful startup.

A graphical client's core-update feature may overwrite the modified executable. If you encounter compatibility problems, restore the backed-up original `xray.exe`.

### Disable the optimization

Set the environment variable in PowerShell and restart Xray:

```powershell
$env:XRAY_WINOPT_MODE = 'off'
.\xray.exe run -config .\config.json
```

## Traffic behavior and compatibility

Changes to receive-buffer management may affect advertised TCP receive windows, ACK timing, and packet timing. For the traffic-analysis implications, see the [TCP fingerprint risk audit](docs/FINGERPRINT-AUDIT.md).

This project focuses on receive-window tuning for Windows TCP outbound connections. Results also depend on congestion, RTT, packet loss, and server conditions.

## Related documentation

- [Technical design, test results, and change history](docs/TECHNICAL.md)
- [Windows setup and rollback](docs/INSTALL-WINDOWS.md)
- [TCP traffic fingerprint risk audit](docs/FINGERPRINT-AUDIT.md)
- [GitHub build and publishing guide](docs/PUBLISH-GITHUB.md)
- [简体中文 README](README.md)

This is an independent, unofficial project and is not affiliated with [XTLS/Xray-core](https://github.com/XTLS/Xray-core).
