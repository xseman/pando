# The hidden start of every tape: a fresh demo repository, pando's state
# beside it, none of the recording terminal's env, and cfg to write
# config.toml with. $DEMO points back at docs/demo; the tape stays hidden
# until it says Show.
#
# REPO and STATE come from the environment, so two tapes can record at once
# without wiping each other's repository or reaching each other's daemon; the
# Makefile sets them per tape. A bare `vhs some.tape` gets the defaults. The
# repository is always named pando-demo, whatever directory holds it, because
# that name is what the tree and the status bar show.
Hide
Type "export DEMO=$PWD/docs/demo REPO=${PANDO_DEMO_REPO:-/tmp/pando-demo} STATE=${PANDO_DEMO_STATE:-/tmp/pando-state}" Enter
Type "export PANDO_RUNTIME_DIR=$STATE/run PANDO_CONFIG_DIR=$STATE/cfg PANDO_DATA_DIR=$STATE/data" Enter
Type "unset ALACRITTY_SOCKET CLAUDE_CODE_CHILD_SESSION" Enter
Type "pando stop >/dev/null 2>&1; rm -rf $STATE; bash $DEMO/setup.sh $REPO >/dev/null && cd $REPO" Enter
Type "source $DEMO/cfg.sh" Enter
