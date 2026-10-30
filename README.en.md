# Xray-WinOptimize

**An experimental, unofficial Windows TCP outbound performance optimization for Xray-core.** Inspired by Linux TCP receive-buffer autotuning design principles, the project adapts Windows socket receive-buffer requests to connection conditions while retaining the native Windows TCP stack. It does **not** port the Linux TCP stack or reproduce Linux kernel autotuning.

## Scope of the observation

**The reported problem with Windows receive-window autotuning under congestion has only been observed by the project author on their own Windows machine, network, and particular VPS setup.** It must not be generalized to all Windows installations or networks, or presented as proof of a widespread Windows TCP defect.

**In that specific congested network and VPS test setup**, the author observed download throughput increasing from approximately **40 Mbps to 70 Mbps** when using the modified core. This is **one environment's observation**, not a benchmark applicable to other users or a promised performance improvement. Results may be unchanged or worse elsewhere.

## Important caveats

- The default build still uses Windows' native TCP stack and ordinarily requires no administrator privileges or network drivers. The optimization applies to outbound connections using Xray's default TCP dialer, **not all traffic on Windows**.
- Changing socket receive-buffer management may subtly affect **advertised TCP receive windows, ACK timing, and packet timing**, potentially exposing additional traffic-behavior features to passive observers. It has **not** been demonstrated that these changes are unique to Xray or that they enable a reliable classifier; the project does **not** promise unidentifiability. See the [fingerprint security audit](docs/FINGERPRINT-AUDIT.md).
- This remains experimental. Back up the existing core and configuration. For a fair adaptive test, remove manually configured outbound `SO_RCVBUF` options.

## Downloads and automatic releases

Every other day, GitHub Actions checks the latest published Xray-core Release (prereleases included). If no complete WinOptimize Release for that upstream version exists, it builds all five Windows targets and publishes a GitHub [Release](https://github.com/Vikrant-Aleph/Xray-WinOptimize/releases) **only after every target succeeds**:

- `Xray-winopt-win7-32.zip` — Windows 7 x86 (patched Go SDK)
- `Xray-winopt-win7-64.zip` — Windows 7 x64 (patched Go SDK)
- `Xray-winopt-win-32.zip` — modern Windows x86
- `Xray-winopt-win-64.zip` — modern Windows x64
- `Xray-winopt-win-arm-64.zip` — Windows ARM64

Each ZIP contains only **`xray.exe`**, `LICENSE`, and `BUILD-INFO.txt`. Project READMEs and technical documentation remain in the GitHub repository, not in the binary archives. To build manually, run **Actions → Xray-WinOptimize (latest upstream)** with an empty version input.

**Win7 warning:** These builds rely on [XTLS/go-win7](https://github.com/XTLS/go-win7) and system updates. The Windows `SIO_TCP_INFO` API required by the adaptive optimization is not guaranteed on Windows 7, so a Win7 binary might start without the optimization working. Runtime and network compatibility have not been verified. A successful build does not prove support.

## Run

**Command line:**

```powershell
.\xray.exe run -config .\config.json
```

**Graphical clients (e.g. v2rayN):**

1. Fully exit the client and stop the Xray process. Back up the existing `xray.exe`.
2. Locate the Xray core used by the client (commonly `bin\xray\xray.exe` within a Windows v2rayN installation; layouts vary).
3. Copy the archive's **`xray.exe`** into the Xray core directory, replacing only the core binary. Do not replace the GUI program or delete other core assets.
4. Restart the client, select the **Xray** core if necessary, and confirm startup in its logs. Restore the backed-up core if anything fails.

A GUI client's own **Update Core** action may overwrite the modified binary and require replacing it again.

To disable the optimization for a command-line run, set `XRAY_WINOPT_MODE=off` and restart the process. The default cross-connection cache retains RTT path hints, **not** the previous connection's receive-buffer size before SYN.

See [technical documentation and change history](docs/TECHNICAL.md), [Windows setup](docs/INSTALL-WINDOWS.md), [traffic fingerprint audit](docs/FINGERPRINT-AUDIT.md), and [publishing instructions](docs/PUBLISH-GITHUB.md). Not affiliated with the official [XTLS/Xray-core](https://github.com/XTLS/Xray-core) project.
