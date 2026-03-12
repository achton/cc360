Output demo.gif
Set Width 1400
Set Height 750
Set Theme "Catppuccin Mocha"
Set FontFamily "Hack"
Set FontSize 16
Set Padding 10
Set TypingSpeed 50ms

# Launch the TUI off-screen, then show once it's rendered
Hide
Type "cc360 --demo"
Enter
Sleep 2s
Show

# Pause on the main view
Sleep 3s

# Navigate down to show worktree sessions (⌥ indicators in folder column)
Down
Sleep 1s
Down
Sleep 1s
Down
Sleep 1s
Down
Sleep 1s
Down
Sleep 2s

# Toggle detail pane off
Tab
Sleep 2s

# Toggle detail pane back on
Tab
Sleep 2s

# Open text filter
Type "/"
Sleep 1s
Type@100ms "api"
Sleep 3s

# Clear filter
Escape
Sleep 2s

# Open project picker — shows worktree names
Type "p"
Sleep 3s

# Navigate picker and select a group
Down
Sleep 800ms
Down
Sleep 800ms
Space
Sleep 1s
Enter
Sleep 3s

# Clear project filter and quit
Escape
Sleep 2s
Type "q"
