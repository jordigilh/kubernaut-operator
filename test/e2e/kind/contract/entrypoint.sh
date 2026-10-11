#!/bin/sh

set -eu

# The migration command is deliberately inert. The Kind journey injects the
# completed Job status after the operator creates it, so no database migration
# or upstream application image is required for this operator contract lane.
if [ "${0##*/}" = "goose" ]; then
	while :; do
		sleep 3600
	done
fi

# This image is deliberately not an application image. It provides only the
# protocol surface required by production-generated workloads: health probes on
# 8081 and, when a serving Secret is mounted, a TLS endpoint on 8443. The
# operator contract lane therefore verifies manifests, trust, and identity
# without qualifying upstream Kubernaut application behavior.
document_root=/opt/kind-contract-http

darkhttpd "$document_root" --port 8081 --addr 0.0.0.0 --no-listing &
http_pid=$!

tls_pid=""
tls_material=""

cleanup() {
	kill "$http_pid" 2>/dev/null || true
	if [ -n "$tls_pid" ]; then
		kill "$tls_pid" 2>/dev/null || true
	fi
}
trap cleanup INT TERM EXIT

while :; do
	if ! kill -0 "$http_pid" 2>/dev/null; then
		exit 1
	fi
	# Reinstall can repair an existing Deployment while its Pod survives, and
	# Secret volumes refresh asynchronously. Unlike the upstream TLS watcher,
	# s_server reads material only at startup. Refresh the fixture's server when
	# the mounted pair changes; never turn off certificate verification or TLS.
	if [ -r /etc/tls/tls.crt ] && [ -r /etc/tls/tls.key ]; then
		if current_material=$(openssl dgst -sha256 /etc/tls/tls.crt /etc/tls/tls.key 2>/dev/null); then
			if [ "$current_material" != "$tls_material" ] || ! kill -0 "$tls_pid" 2>/dev/null; then
				if [ -n "$tls_pid" ]; then
					kill "$tls_pid" 2>/dev/null || true
					wait "$tls_pid" 2>/dev/null || true
				fi
				openssl s_server -accept 8443 -cert /etc/tls/tls.crt -key /etc/tls/tls.key -www \
					>/dev/null 2>&1 &
				tls_pid=$!
				tls_material=$current_material
			fi
		fi
	fi
	sleep 1
done
