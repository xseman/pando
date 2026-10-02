# The terminal every tape records in. A tape sources this first and may
# override a setting right after it.
Set Shell bash
Set FontFamily "JetBrainsMono Nerd Font Mono"
Set FontSize 20
# vhs subtracts padding and margin from these, so they carry both: the terminal
# itself stays 1200x720, 98 columns, the size the tapes count columns against
# (side by side wants a 90 column main area, beside a hidden sidebar's icons).
Set Width 1280
Set Height 800
Set Padding 20
Set Margin 20
Set MarginFill "#674EFF"
Set BorderRadius 10
Set TypingSpeed 40ms
Set Framerate 24

# The palette. vhs's own default is the dark one the GIFs record in; uncomment
# the light line and flip `mode` in cfg.sh with it, since this paints the
# terminal while `mode` paints pando and the claude session inside it.
# Set Theme "Catppuccin Latte"
