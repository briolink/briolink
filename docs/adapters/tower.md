# Adapter: the tower (ops host)

Current binding for the **ops host** role ([bindings.md](../bindings.md)): an always-on Windows 11 machine (i5 10th gen, 12 threads, 32 GB RAM, 16 GB GPU). Its location is kept in the operator config, not in this repo; data-protection filings take it from there. It runs the control plane, the warm standby, the coder model and the private-network hub. Development also happens here, in WSL2.

## Setup

- **WSL2:** capped at 16 GB / 8 threads with mirrored networking ([../../scripts/wslconfig.example](../../scripts/wslconfig.example)). ~9 GB of that covers the standby at full capacity. WSL starts outside the login session and starts automatically (decision 12).
- **Dev tools:** [../../scripts/setup-wsl-dev.sh](../../scripts/setup-wsl-dev.sh).
- **Hardware:** BitLocker on the drive holding database copies; a UPS if possible.

## Sharing with the coder model and workers

- Ollama runs on Windows: `qwen3-coder:30b` sits mostly in the GPU's 16 GB, with ~2 GB spilled to system RAM.
- GPU and RAM don't compete with hosting, but **CPU can**: the spilled layers and the workers' builds and tests all use the CPU.
- **On a failover takeover,** the takeover script pauses the assessment and build workers and lowers Ollama's priority and thread count.
- The failover drill runs while the model is generating, to confirm API latency stays normal.

## Network (private-network hub)

- **Hub:** UDP 51820 forwarded on the local router to the hub. A DNS-only record (`wg.<brand domain>`) is kept current by a DDNS job.
- **Risks (spikes):**
  - the ISP may use CGNAT, which blocks the forward; the fallback is to move the hub to a €3–5 VPS;
  - the WSL2 mirrored-networking hub may need a Hyper-V firewall rule.
- **ISP terms:** check that the connection may be used as a short-term fallback server and as a network hub.
- **Limits:** the connection is a fallback for hours, not a long-term host ([servers.md](../servers.md#failover)).

## Spikes

- [ ] The ISP gives a public IPv4 (no CGNAT) and the router forwards the hub's UDP port
- [ ] The hub inside WSL2 with mirrored networking receives forwarded traffic and routes between peers
- [ ] Spokes recover after a public IP change within ~2 min

## Swap notes

Any always-on Linux machine with a GPU for the coder model can take the role, owned or rented. Without a GPU, the coder model moves to a separate GPU host reached over the private network.
