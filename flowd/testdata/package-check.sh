#!/bin/bash
# Runs inside the container package-test.sh boots. Installs the deb, puts a
# tailnet client in a netns behind a veth named tailscale0, masquerades it
# out of eth0, and checks the report the sandboxed unit spools on stop.
# There is no tailscaled, so the report stays in the spool.
set -euo pipefail

until systemctl is-system-running 2>/dev/null | grep -qE 'running|degraded'; do sleep 1; done

echo 0 >/proc/sys/net/netfilter/nf_conntrack_acct

dpkg -i /opt/slopscale-flowd.deb
systemd-analyze verify /usr/lib/systemd/system/slopscale-flowd.service
systemd-analyze security --no-pager slopscale-flowd.service | tail -1

ip netns add client
ip link add tailscale0 type veth peer name c0
ip link set c0 netns client
ip addr add 100.64.10.1/24 dev tailscale0
ip link set tailscale0 up
ip -n client addr add 100.64.10.2/24 dev c0
ip -n client link set c0 up
ip -n client route add default via 100.64.10.1
echo 1 >/proc/sys/net/ipv4/ip_forward
nft add table ip flowtest
nft add chain ip flowtest post '{ type nat hook postrouting priority 100; }'
nft add rule ip flowtest post oifname eth0 masquerade

mkdir -p /etc/systemd/system/slopscale-flowd.service.d
cat >/etc/systemd/system/slopscale-flowd.service.d/test.conf <<'EOF'
[Service]
ExecStart=
ExecStart=/usr/bin/slopscale-flowd --server http://127.0.0.1:9 --verbose
EOF
systemctl daemon-reload
systemctl restart slopscale-flowd
sleep 3
systemctl is-active slopscale-flowd
test "$(cat /proc/sys/net/netfilter/nf_conntrack_acct)" = 1
# Go cannot raise its own soft limit under SystemCallFilter=~@resources.
pid=$(systemctl show -p MainPID --value slopscale-flowd)
grep -E '^Max open files +65536 +65536' "/proc/$pid/limits"

addr=$(getent ahostsv4 example.com | awk 'NR==1 { print $1 }')
ip netns exec client curl -sS -o /dev/null --resolve "example.com:443:$addr" https://example.com/

# Well inside the 30s dump interval: only the dump on stop can count it.
systemctl stop slopscale-flowd
journalctl -u slopscale-flowd --no-pager -o cat

zstd -qdc /var/lib/slopscale-flowd/spool/*.json.zst | jq -c . | tee /tmp/reports.json
jq -se '
  [.[].status | .conntrack, .sni] | all(.enabled and (.error == null))
' /tmp/reports.json >/dev/null
jq -se --arg addr "$addr" '
  [.[].flows[]? | select(.src == "100.64.10.2" and .dst == $addr and .port == 443)]
  | length == 1 and .[0].host == "example.com" and .[0].hostSource == "sni"
    and .[0].txBytes > 0 and .[0].rxBytes > 0 and .[0].conns == 1
' /tmp/reports.json >/dev/null

echo "PASS: the packaged unit counted and named the request"

# The maintainer scripts behave like dh_installsystemd's: removal stops and
# masks the unit, a reinstall unmasks and starts it, purge leaves nothing.
# Docker's Debian images forbid maintainer scripts to start or stop
# services; a real system does not.
rm -f /usr/sbin/policy-rc.d
unit=/etc/systemd/system/slopscale-flowd.service
systemctl start slopscale-flowd
dpkg -r slopscale-flowd
test "$(systemctl is-active slopscale-flowd || true)" != active
test "$(readlink "$unit")" = /dev/null

dpkg -i /opt/slopscale-flowd.deb
test ! -e "$unit"
systemctl is-enabled slopscale-flowd
systemctl is-active slopscale-flowd

dpkg -P slopscale-flowd
test ! -e "$unit"
test ! -e /var/lib/slopscale-flowd
test -z "$(find /var/lib/systemd/deb-systemd-helper-enabled -name '*slopscale-flowd*' 2>/dev/null)"
# The accounting sysctls stay on until the next boot, by choice.
test "$(cat /proc/sys/net/netfilter/nf_conntrack_acct)" = 1

echo "PASS: remove, reinstall and purge"
