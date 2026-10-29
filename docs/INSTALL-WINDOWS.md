# Xray-WinOptimize: Windows experimental test procedure

For source changes, design decisions, risks, verification status and handoff, see [TECHNICAL.md](TECHNICAL.md). The README files provide only a brief overview and installation entrypoint.

**NOT production-ready.** A kernel-level packet interception driver can interrupt network traffic. Save the known-good `xray.exe`, config and logs. The experimental backend is **off by default**.

1. Compile a patched Xray on Windows (or use a GitHub Actions artifact):

   ```powershell
   git clone --branch v26.10.21 --depth 1 https://github.com/XTLS/Xray-core.git xray-src
   python patch/apply.py .\xray-src
   cd .\xray-src
   go build -trimpath -buildvcs=false -o xray-winrecv-experimental.exe ./main
   ```

   Requires the Go version declared by upstream `go.mod` (Go 1.27 or newer for v26.10.21).

2. Obtain **official** [WinDivert v2.2.2](https://github.com/basil00/WinDivert/releases/tag/v2.2.2). For x64 Windows, place its x64 `WinDivert.dll` and **signed** `WinDivert64.sys` beside `xray-winrecv-experimental.exe`. These are **not bundled** by this project's build artifacts. Comply with WinDivert's LGPLv3/GPLv2 license options if redistributing. Do not use random third-party driver downloads.

3. Remove the experimental `SO_RCVBUF` custom socket option from the outbound Xray config. Run an **administrator** PowerShell only for this experimental packet-driver test:

   ```powershell
   $env:XRAY_EXPERIMENTAL_TCP_STACK = '1'
   .\xray-winrecv-experimental.exe run -config .\config.json
   ```

   Leave the SOCKS/HTTP inbound configuration unchanged. The patch replaces default Xray outbound TCP connections only. UDP continues on Winsock. Outbound TCP with unsupported custom socket features fails closed; this is intentional.

4. Test a single TCP connection first, then REALITY handshake, then a speed test. Expect `WinDivertOpen` / source-port-reservation errors on unsupported hosts. Do **not** assume a successful Go build proves the driver works.

5. Rollback: stop experimental Xray and clear the environment variable, or run the untouched official Xray binary:

   ```powershell
   Remove-Item Env:\XRAY_EXPERIMENTAL_TCP_STACK -ErrorAction SilentlyContinue
   ```

## Limitations to address before ordinary use

- End-to-end WinDivert integration and Windows 11 26H2 network tests have **not** been done; this is a research prototype, not a validated solution.
- Source-port reservation and WinDivert handle behavior require real traffic validation. In particular, failure to reserve a source port may result in a timed-out connection in the current Xray TCP implementation.
- Uses one WinDivert handle per TCP flow; this costs resources at high connection counts (temporary hard limit: 128 flows per source-IP stack).
- IPv4/IPv6 *ordinary* TCP packets supported; IPv6 extension headers, fragmented IP traffic, complex source-bound socket options, multicast, link-local routes, and multi-adapter policy routing are incomplete.
- Xray uses per-source-IP user-mode stacks; VPN, NAT, routing changes, firewalls, third-party network filters and sleep/resume must be tested.
- The Linux-style controller is a *simplified adaptation*, not the full Linux TCP receive algorithm or a port of its memory accounting.
- This prototype is not ready to be labeled a general-purpose replacement for the operating system's entire TCP stack; it targets Xray's default TCP outbound dialer.
