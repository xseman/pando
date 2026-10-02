# The terminal every tape records in. A tape sources this first and may
# override a setting right after it.
Set Shell bash
Set FontFamily "JetBrainsMono Nerd Font Mono"
Set FontSize 20
# vhs subtracts padding and margin from these, so they carry both: the terminal
# itself stays 1200x720, 98 columns, the size the tapes count columns against
# (side by side wants a 90 column main area, beside a hidden sidebar's icons).
Set Width 1320
Set Height 840
Set Padding 20
Set Margin 40
# A blue gradient, which `make demo` builds (make docs/demo/bg.png for a bare vhs run).
Set MarginFill "docs/demo/bg.png"
Set BorderRadius 10
Set TypingSpeed 40ms
Set Framerate 24

# The palette: VS Code's light terminal colors on a white editor. Keep it in
# step with `mode` in cfg.sh, since this paints the terminal while `mode`
# paints pando and the claude session inside it; for the dark GIFs, drop this
# line (vhs's own default is dark) and flip `mode`.
Set Theme { "name": "VS Code Light", "background": "#ffffff", "foreground": "#3b3b3b", "selection": "#add6ff", "cursor": "#000000", "cursorAccent": "#ffffff", "black": "#000000", "red": "#cd3131", "green": "#107c10", "yellow": "#949800", "blue": "#0451a5", "magenta": "#bc05bc", "cyan": "#0598bc", "white": "#555555", "brightBlack": "#666666", "brightRed": "#cd3131", "brightGreen": "#14ce14", "brightYellow": "#b5ba00", "brightBlue": "#0451a5", "brightMagenta": "#bc05bc", "brightCyan": "#0598bc", "brightWhite": "#a5a5a5" }
