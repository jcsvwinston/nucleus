# systemd unit templates

`nucleus-admin-server.service` is a starting point for running the
admin observability server on a bare-metal or VM host. It assumes:

* A `nucleus` system user with no shell.
* The binary at `/usr/local/bin/admin-server`.
* TLS material under `/etc/nucleus/`.
* An optional `EnvironmentFile=/etc/nucleus/admin-server.env` for
  the agent shared token.

Install:

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin nucleus
sudo install -m 0755 bin/admin-server /usr/local/bin/admin-server
sudo install -m 0755 -d /var/log/nucleus
sudo chown nucleus:nucleus /var/log/nucleus

sudo install -m 0644 examples/admin-quickstart/systemd/nucleus-admin-server.service \
    /etc/systemd/system/

sudo systemctl daemon-reload
sudo systemctl enable --now nucleus-admin-server
sudo systemctl status nucleus-admin-server
journalctl -u nucleus-admin-server -f
```

For a multi-host deployment (active-passive), install the unit on both
hosts and configure the agents on every framework host with the two
endpoint URLs in `admin.endpoints`. The agent's failover logic picks
the first reachable endpoint and reconnects when it stops responding.

The hardening directives in the unit (`ProtectSystem`,
`MemoryDenyWriteExecute`, `SystemCallFilter`, ...) are conservative
and may need tweaking for your distro. Validate with
`systemd-analyze security nucleus-admin-server` after installation.
