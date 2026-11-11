# Sourced by usage.tape before cfg: plan limits made up for the demo, so no
# real account's use reaches a GIF. claude's sign-ins hold fake tokens, which
# a local proxy answers for api.anthropic.com under a throwaway CA the daemon
# trusts (SSL_CERT_FILE); a fake codex answers its app-server's question.
u=$STATE/usage
mkdir -p "$u/claude" "$u/claude-work" "$u/codex"
exp=$(( ($(date +%s) + 86400) * 1000 ))
printf '{"claudeAiOauth": {"accessToken": "demo-default", "expiresAt": %s, "subscriptionType": "max", "rateLimitTier": "default_claude_max_5x"}}\n' "$exp" > "$u/claude/.credentials.json"
printf '{"claudeAiOauth": {"accessToken": "demo-work", "expiresAt": %s, "subscriptionType": "team", "rateLimitTier": "default_claude_max_5x"}}\n' "$exp" > "$u/claude-work/.credentials.json"
echo '{}' > "$u/codex/auth.json"
(
	cd "$u" || exit
	openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.pem -days 2 -subj /CN=pando-demo-ca \
		-addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign
	openssl req -newkey rsa:2048 -nodes -keyout srv.key -out srv.csr -subj /CN=api.anthropic.com
	printf 'subjectAltName=DNS:api.anthropic.com\nextendedKeyUsage=serverAuth\n' > srv.ext
	openssl x509 -req -in srv.csr -CA ca.pem -CAkey ca.key -CAcreateserial -out srv.pem -days 2 -extfile srv.ext
) >/dev/null 2>&1
# Gone on its own after the recording: nothing stops it otherwise. One left
# from an earlier run holds the port with a CA that is gone.
pkill -f '^python3 -I .*/usage/proxy[.]py' && while pgrep -f '^python3 -I .*/usage/proxy[.]py' >/dev/null; do sleep 0.1; done
setsid timeout 600 python3 -I "$DEMO/usage/proxy.py" 18443 "$u/srv.pem" "$u/srv.key" >/dev/null 2>&1 < /dev/null &
export PATH=$DEMO/usage/bin:$PATH CODEX_HOME=$u/codex HTTPS_PROXY=http://127.0.0.1:18443 SSL_CERT_FILE=$u/ca.pem
demo_claude_src=$u/claude
