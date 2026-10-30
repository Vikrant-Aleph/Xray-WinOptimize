# Xray-WinOptimize

[简体中文](README.md) | English

On **my own Windows PC, network connection, and VPS setup**, I noticed that **when the network was congested and latency was high, Windows TCP receive-window autotuning sometimes did not expand the receive window quickly enough, and Xray download throughput dropped significantly**. Manually increasing `SO_RCVBUF` helped, but keeping a fixed receive-buffer size is not an ideal general-purpose solution. **That observation led me to create Xray-WinOptimize.**

Xray-WinOptimize is an **unofficial, experimental Windows TCP outbound performance optimization for Xray-core**. Inspired by Linux TCP receive-buffer autotuning principles, it adjusts Windows socket receive-buffer requests based on connection conditions while **retaining the native Windows TCP stack**. Its goal is to improve download throughput in certain congested, high-latency environments.

> **Scope of this observation:** The behavior above was observed only on my own hardware, network, and specific VPS test setup. It should not be generalized to every Windows installation or treated as evidence of a widespread Windows TCP defect.

## Why this project exists: observations and results

During investigation, I found that the Windows receive buffer **was not completely failing to autotune**: it increased over time, but in some congested conditions its growth was too slow or its size remained too small to make full use of the available bandwidth during a short download. Explicitly increasing `SO_RCVBUF` improved both the receive-window behavior and download throughput in my tests. This motivated an adaptive approach inside Xray rather than requiring users to hard-code a receive-buffer size indefinitely.

In **one comparison using my own network and VPS test setup**, download throughput with the modified Xray-core increased from approximately **40 Mbps to 70 Mbps**.

This is a result from one environment and test period, **not a general speedup ratio or performance guarantee**. Actual results depend on the route, RTT, loss, server, connection concurrency, and Windows version. Other environments may show no improvement or even worse performance.

## What the optimization does—and does not do

- **Scope:** Windows outbound TCP connections established through Xray's default TCP dialer—not all traffic on the computer.
- **Approach:** Uses native Windows socket interfaces, including `SO_RCVBUF`, to adjust the receive buffer according to connection conditions. It does not replace the Windows TCP stack.
- **Requirements:** Normally needs no additional network driver, administrator privileges, or system-wide TCP configuration changes.
- **Design boundary:** It borrows ideas from Linux receive-buffer autotuning, but **does not** port Linux TCP or reproduce the Linux kernel's autotuning algorithm.
- **Cross-connection hints:** By default, only historical path hints such as RTT are retained. The receive-buffer size of one connection is **not** directly carried over to the next connection before SYN.

For algorithm details, configuration, source changes, and experimental limitations, see the [technical documentation](docs/TECHNICAL.md).

## Downloads and automatic releases

Download a build for your architecture from [GitHub Releases](https://github.com/Vikrant-Aleph/Xray-WinOptimize/releases):

| Release archive | Target platform |
| --- | --- |
| `Xray-winopt-win7-32.zip` | Windows 7 x86 (special compatibility toolchain) |
| `Xray-winopt-win7-64.zip` | Windows 7 x64 (special compatibility toolchain) |
| `Xray-winopt-win-32.zip` | Modern Windows x86 |
| `Xray-winopt-win-64.zip` | Modern Windows x64 |
| `Xray-winopt-win-arm-64.zip` | Windows ARM64 |

**Each ZIP contains only `xray.exe`, `LICENSE`, and `BUILD-INFO.txt`.** READMEs and other documentation stay in this repository rather than being duplicated inside binary packages. The executable is always named `xray.exe`, so no renaming is needed.

Every **other day**, GitHub Actions checks the latest published Xray-core Release (including prereleases, excluding drafts). If a complete WinOptimize Release for that upstream version is missing, the workflow attempts to build all five targets. A new Release is published **only after all five builds succeed and pass architecture checks**; older binaries are not presented as builds of a newer version. Developers can also run `Xray-WinOptimize (latest upstream)` manually from [Actions](https://github.com/Vikrant-Aleph/Xray-WinOptimize/actions), leaving the version input empty to select the latest upstream Release.

> **Windows 7 warning:** Standard Go no longer supports Windows 7. These builds use the [XTLS/go-win7](https://github.com/XTLS/go-win7) compatibility toolchain and require relevant system updates. The adaptive optimization relies on `SIO_TCP_INFO`, which is not guaranteed to be available on Windows 7; **a successful build or launch does not prove that the optimization works**. Windows 7 runtime and networking compatibility have not been verified on physical machines. Actual compatibility on other targets also requires testing.

## Installation and usage

Back up your existing Xray binary and configuration before replacing the core so you can revert if there are connection issues. If you previously configured an outbound `SO_RCVBUF` manually, remove that option before comparing adaptive behavior to avoid mixing test conditions.

### Command line

Put the ZIP's `xray.exe` in your existing Xray runtime directory and start it with your current configuration:

```powershell
.\xray.exe run -config .\config.json
```

### Replace the core in a graphical client (for example, v2rayN)

1. **Fully exit** the graphical client, stop the Xray process, and back up the existing `xray.exe`.
2. Locate the actual Xray core directory used by the client. On Windows, v2rayN commonly stores it under `bin\xray\`, but layouts vary.
3. Copy the release's `xray.exe` into that directory, replacing the core binary. **Do not replace the GUI application's executable or delete other required files.**
4. Restart the client, make sure the Xray core is selected, and check its logs for successful startup. Restore the original `xray.exe` if anything fails.

A graphical client's **Update Core** feature may overwrite the modified executable, requiring it to be replaced again.

### Temporarily disable the optimization

For a command-line run, set the following environment variable in the current PowerShell session and restart Xray:

```powershell
$env:XRAY_WINOPT_MODE = 'off'
.\xray.exe run -config .\config.json
```

## Limitations and traffic-behavior risks

This project remains experimental. Changing socket receive-buffer behavior may also change observable **advertised TCP receive windows, ACK timing, and packet timing**, potentially adding features that passive traffic analysis could use. However, **there is no evidence that these changes are unique Xray fingerprints or enable a reliable identification method**. The project does not promise unidentifiability. See the [TCP fingerprint risk audit](docs/FINGERPRINT-AUDIT.md).

This optimization cannot address every cause of low throughput and is no substitute for checking route quality, packet loss, server load, and proxy configuration. Avoid drawing conclusions from a single speed test.

## Documentation and disclaimer

- [Technical design, test notes, and change history](docs/TECHNICAL.md)
- [Windows setup and rollback](docs/INSTALL-WINDOWS.md)
- [TCP traffic fingerprint risk audit](docs/FINGERPRINT-AUDIT.md)
- [GitHub build and publishing guide](docs/PUBLISH-GITHUB.md)
- [简体中文 README](README.md)

This is an independent, unofficial experimental project and is not affiliated with [XTLS/Xray-core](https://github.com/XTLS/Xray-core).
