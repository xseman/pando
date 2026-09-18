# The hidden start of every tape: pando's state under /tmp/pando-state, a fresh
# demo repository in /tmp/pando-demo, none of the recording terminal's env, and
# cfg to write config.toml with. $DEMO points back at docs/demo; the tape stays
# hidden until it says Show.
Hide
Type "export DEMO=$PWD/docs/demo PANDO_RUNTIME_DIR=/tmp/pando-state/run PANDO_CONFIG_DIR=/tmp/pando-state/cfg PANDO_DATA_DIR=/tmp/pando-state/data" Enter
Type "unset ALACRITTY_SOCKET CLAUDE_CODE_CHILD_SESSION" Enter
Type "pando stop >/dev/null 2>&1; bash $DEMO/setup.sh >/dev/null && cd /tmp/pando-demo" Enter
Type "source $DEMO/cfg.sh" Enter
