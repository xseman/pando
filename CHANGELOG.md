# Changelog

## [0.6.0](https://github.com/xseman/pando/compare/v0.5.0...v0.6.0) (2026-10-17)


### Features

* **daemon:** keep named databases in config.toml's [databases] ([6db19f0](https://github.com/xseman/pando/commit/6db19f0e44859bbaebaf7d9633c1b9d4781524c9))
* **daemon:** keep pinned Spaces sessions in state.json ([38f2fdb](https://github.com/xseman/pando/commit/38f2fdb0a4463c73c950d32b5e4852cac8c90f57))
* **daemon:** tell a screen's width and the directory in front ([bbe71e6](https://github.com/xseman/pando/commit/bbe71e63e7671131f82cf99a8414b07180e58aca))
* **db:** read and edit SQLite, Postgres and MariaDB through their clients ([f6b0fd8](https://github.com/xseman/pando/commit/f6b0fd84d3a28d66e58a9e5f939844d947f2f8ff))
* **git:** list files by name pattern, ignored ones too ([ecb6f44](https://github.com/xseman/pando/commit/ecb6f4438ea20793eb36d32fe5b600445abcde0e))
* keep a scrolled-back session page on its text ([db3ff64](https://github.com/xseman/pando/commit/db3ff6499fe9e3fc45ebb886f78aa7e0c7ba3fdc))
* run separate pando instances with --instance ([292eeb6](https://github.com/xseman/pando/commit/292eeb66f182f3b2c2f3a8747cd68c0138189b0b))
* show language servers' diagnostics ([7a2cdca](https://github.com/xseman/pando/commit/7a2cdca64b304c2b5ccb8f9e82ddcfd956e290f3))
* **ui:** add a Databases view and a table grid ([375047f](https://github.com/xseman/pando/commit/375047fe03c1c6b1ca48c5a2bbe7f6c6eede4d18))
* **ui:** add a tooltips setting to turn the button tips off ([22e71c0](https://github.com/xseman/pando/commit/22e71c0c9ad5ecc7d3f94b9b7eac905e5cf5cdec))
* **ui:** dock a session beside an empty editor ([62ca895](https://github.com/xseman/pando/commit/62ca895c89cc016b074cb5999cc45ac8e5f4d881))
* **ui:** draw a border over the bottom Terminal panel ([cd1f220](https://github.com/xseman/pando/commit/cd1f2206095b3bda281d193f750e7c20dde60abc))
* **ui:** draw a rule over each open Spaces project ([c58f316](https://github.com/xseman/pando/commit/c58f31670b18d7d5e90b59dea00f67e1414e614d))
* **ui:** draw indent guides in every tree ([f748829](https://github.com/xseman/pando/commit/f748829ae78b697daa3a0a694ed97632c142e704))
* **ui:** draw indent guides in the editor ([9bb7152](https://github.com/xseman/pando/commit/9bb715216d80f5ca70a4326ea5a58b79d30cbf7f))
* **ui:** draw indent guides in the Explorer tree ([351f079](https://github.com/xseman/pando/commit/351f0792dcbe622a3ca48f571b8f27183ca26d00))
* **ui:** expand a diff's hidden lines in place ([32e9cb4](https://github.com/xseman/pando/commit/32e9cb40490a262943ab5974fbe61fe024049523))
* **ui:** frame Spaces' Pinned as a resizable pane, under its worktrees ([638dff9](https://github.com/xseman/pando/commit/638dff9f7f6aed2d9860990e47594afd29ba7fee))
* **ui:** keep a worktree's maximized session, file or Terminal across restarts ([2ec4565](https://github.com/xseman/pando/commit/2ec4565cb5d168b9e1fd019f8c7a3f66e3b2590e))
* **ui:** keep each project's GitHub lists and fetch them again when old ([c6210cb](https://github.com/xseman/pando/commit/c6210cbfe5adcfe5e4a58626685ea0823922cb31))
* **ui:** keep each session's own maximized state ([210669d](https://github.com/xseman/pando/commit/210669d66f1990e95adf1051929802f58f5c39e2))
* **ui:** lay a GitHub pull request or issue out as its conversation ([c4978d6](https://github.com/xseman/pando/commit/c4978d66c5c40ec09c23e0526852c61967328a64))
* **ui:** light status bar items and dialog buttons under the pointer ([d2ae7ca](https://github.com/xseman/pando/commit/d2ae7ca44ac9c5f5648e9408841e6ba47efb2c92))
* **ui:** light the Explorer's active indent guide ([3fdf529](https://github.com/xseman/pando/commit/3fdf529ff461a6ef2bdb3d419d55060f8be25ceb))
* **ui:** mark a diff's changes on its scrollbar ([9c319b1](https://github.com/xseman/pando/commit/9c319b1aafa33c485b482216b72cb1d02149d56b))
* **ui:** mark a file's changes on its scrollbar ([af14a09](https://github.com/xseman/pando/commit/af14a09443414f6111c70c94fa8e1b5c01db0c64))
* **ui:** mark changed lines in the editor's gutter, VS Code's quick diff ([d74a808](https://github.com/xseman/pando/commit/d74a808e1887238c128fa85d3f8995f95e6a95ef))
* **ui:** mark what a picker's query matched, and say when nothing does ([9b03964](https://github.com/xseman/pando/commit/9b03964213c768703e95fcdb8403254b658d5caf))
* **ui:** name an icon button and its key when the pointer rests on it ([da5e520](https://github.com/xseman/pando/commit/da5e5208eda7c7e59dc67fcf854284d51551059b))
* **ui:** open a terminal's links with ctrl+click ([bb9171e](https://github.com/xseman/pando/commit/bb9171ead97c52467a9b17eb9a129001da7f1d55))
* **ui:** open the GitHub view with only pull requests unfolded ([37e3e87](https://github.com/xseman/pando/commit/37e3e8746f5d3a8c5a6cd6422b2bc51f6d8cd78a))
* **ui:** pin sessions on top of Spaces ([021d8c4](https://github.com/xseman/pando/commit/021d8c41b1034f843af14bf13b20c554f9faae7d))
* **ui:** resume a conversation from a worktree in Spaces ([888c6fe](https://github.com/xseman/pando/commit/888c6fe2b33f6dbb205d592fa1ef76ffc27df9f3))
* **ui:** resume from Spaces' header ([b0337a7](https://github.com/xseman/pando/commit/b0337a7fa3356513912e69e3484f2b090ac489b2))
* **ui:** reveal the editor's file in Explorer ([5e5299c](https://github.com/xseman/pando/commit/5e5299c90e7c3c069d8e07146036848ae7240c8a))
* **ui:** say in the merge dialog when a pull request's checks fail ([651060e](https://github.com/xseman/pando/commit/651060e0fbb1d0ad08ec687e2fbf71383db82b74))
* **ui:** set Spaces' Pinned apart by one heavy border, no resize ([d9d641a](https://github.com/xseman/pando/commit/d9d641a1a37fe37b591292550d4824d9eb90f88e))
* **ui:** show a cut session name whole in a tip ([2ce07e6](https://github.com/xseman/pando/commit/2ce07e63cb4056f82102ce3bb0cc687afa92b9e3))
* **ui:** show a GitHub page's button tips by the pointer ([1fbdc4f](https://github.com/xseman/pando/commit/1fbdc4f0730e4c216116093871231a7c5f4b1ff5))
* **ui:** show an agent's state on a tab or terminal that runs it ([7231ea7](https://github.com/xseman/pando/commit/7231ea705563e9c3d6469948a84db33741b3ca39))
* **ui:** show the one last used when the session and the file share the editor ([ddace10](https://github.com/xseman/pando/commit/ddace10c3dc68e3e1b02b0bbd6bd659c53823cc1))
* **ui:** show the Spaces pin button on hover only ([70ced37](https://github.com/xseman/pando/commit/70ced37e8b19719fe9252b2936de4205dc628816))
* **ui:** step through a diff's changes and files ([e2af73d](https://github.com/xseman/pando/commit/e2af73dbb4cefa7fa1966ad010c9ed61426edec2))
* **ui:** turn a working session's ◐ like a spinner ([a4d270d](https://github.com/xseman/pando/commit/a4d270d0cc8965d984562b1d886655dc3c258862))


### Bug Fixes

* **daemon:** order Spaces by input and status, not output ([b93888f](https://github.com/xseman/pando/commit/b93888fe7e91c60987ef79e317a5d9f639577e55))
* **ui:** click Source Control's and Search's buttons where they are drawn ([79cee96](https://github.com/xseman/pando/commit/79cee96b2dc6cdcc3ddb043d886f16288ac7a87b))
* **ui:** draw indent guides over render_whitespace's marks ([b280ef9](https://github.com/xseman/pando/commit/b280ef98446f99c8756bdacef733ffe5525468a3))
* **ui:** fit a row whose keycap emoji outmeasures its truncation ([49750ec](https://github.com/xseman/pando/commit/49750ecbdc42f65fb1dc30c58721159bd5429c8d))
* **ui:** flash one line of a multi-line error ([6706218](https://github.com/xseman/pando/commit/67062184f39e48fed3abd8dd5c56eaf03c4394c8))
* **ui:** keep a morph as wide as its new text, and shorter ([2196cba](https://github.com/xseman/pando/commit/2196cba7639f4b264cd41b2adf56a8ae19c9969e))
* **ui:** keep a side-by-side selection on one half ([1a92321](https://github.com/xseman/pando/commit/1a9232193851790219ebd50ed87bc6672a288de8))
* **ui:** keep a status bar item's tip off the bar in a framed layout ([1b232bf](https://github.com/xseman/pando/commit/1b232bf02fdf8bf1b36f8a5a3a2d1060888e6aff))
* **ui:** keep a worktree's maximized session when switching back to it ([44cce28](https://github.com/xseman/pando/commit/44cce2830fedd13b0de82c873030e627a1edad26))
* **ui:** keep emoji the renderer measures narrower from shifting rows ([8e05c76](https://github.com/xseman/pando/commit/8e05c7659a2e1f8a3d5f7a01d6e79d2bada35575))
* **ui:** keep problems squiggled on a selected side-by-side row ([204f553](https://github.com/xseman/pando/commit/204f5539381ffba37585a78f962dc30b7eb75d6e))
* **ui:** keep the columns while a divider is dragged on a narrow screen ([b5e0ba5](https://github.com/xseman/pando/commit/b5e0ba588d4e8400bbd94e60502f53a5c4448838))
* **ui:** leave an editor clean when undo returns to the saved text ([a151377](https://github.com/xseman/pando/commit/a151377990361bac2677b1b359ce9c1fabbfc40a))
* **ui:** leave the Commit button alone while other git work runs ([b79e616](https://github.com/xseman/pando/commit/b79e616c07376dec937f1dbc32eaae58698fbead))
* **ui:** let a tip follow the pointer from button to button ([a3c7cbc](https://github.com/xseman/pando/commit/a3c7cbc0abec2742f46a36061994f6579178fc8f))
* **ui:** put a tip away on a click's release, and name as the palette does ([bf83c54](https://github.com/xseman/pando/commit/bf83c540e86a25311fb39c55092994f4c9e595af))
* **ui:** raise the quick diff's buttons under the mouse ([c17a313](https://github.com/xseman/pando/commit/c17a313e1c0ea2742e7697902dd3c9935bd26de9))
* **ui:** reveal a change's file in Explorer, over Spaces too ([e3f8c35](https://github.com/xseman/pando/commit/e3f8c35711a5c7ef3a59808d6d2b6a89834bbecd))
* **ui:** take M-p in Search's boxes for Preserve Case ([71b7a22](https://github.com/xseman/pando/commit/71b7a220ea87651155a3d7215b0281cbcb9dd3e7))
* **ui:** turn a plain shell's ◐ only while a command runs, on the clock ([2fdaf3e](https://github.com/xseman/pando/commit/2fdaf3e6ceef10c3af56d77d49d6e0c90325ef13))


### Performance Improvements

* **daemon:** keep a session's scrollback as rendered rows ([f0799d0](https://github.com/xseman/pando/commit/f0799d0dfbb007c712b0b6992bd86dc98dbb9a02))
* **git:** read status in one walk, and back off on slow worktrees ([8e8166a](https://github.com/xseman/pando/commit/8e8166a1578bda4c58ff41460b7b95f5df0e74ab))
* **ui:** draw a frame with less work ([73e3318](https://github.com/xseman/pando/commit/73e33188fc64a829a47a1e1179d7279e1509f8f6))
* **ui:** look up a session's links in linear time ([5693a37](https://github.com/xseman/pando/commit/5693a37bd17e3ba7e6fe4a6768d9891a2dbb15bb))
* **ui:** reuse the frame after events that change nothing ([b0538e0](https://github.com/xseman/pando/commit/b0538e043b249c21c0fd62ca251d79c370fc8cd9))
* **ui:** type in large files without matching lexers again ([028eb74](https://github.com/xseman/pando/commit/028eb745153fc89dc5e20d549a416f312954b9b4))


### Documentation

* **config:** describe spaces_project_sort by input and status ([da5d282](https://github.com/xseman/pando/commit/da5d282c83c6804f65865d616a3191df46e1cef8))
* **demo:** drop the CLI GIF and re-record the rest ([1730077](https://github.com/xseman/pando/commit/1730077b8434e951863daba3f2c0bb2f37145281))
* **demo:** record the Databases view ([0b157ae](https://github.com/xseman/pando/commit/0b157ae295aa5ad394ab92c9313c04caec85936c))
* describe the quick diff as a zone under its change ([20dd781](https://github.com/xseman/pando/commit/20dd78146dd535f7c30cf02ca04e67f89b7745b8))


### Maintenance

* run the database tests against Postgres and MariaDB ([baf34f3](https://github.com/xseman/pando/commit/baf34f3bc0e27a0b087bb2a2c60ecf455a060a70))
* **ui:** name the two-way choice and the tip's wake plainly ([9c9ce39](https://github.com/xseman/pando/commit/9c9ce39a8e233b0d5fb8b2424f108dfa998f2b74))
* **ui:** share the Explorer's active indent guide ([3f8119a](https://github.com/xseman/pando/commit/3f8119a924930d26e18b516d4d0e7881c69439c5))


### Testing

* add benchmarks, allocation budgets and profile targets ([d49c44c](https://github.com/xseman/pando/commit/d49c44cea0851b9cfff6e0da538f0b09a26613c4))
* **ui:** drop the view before renaming the table it reads ([e051062](https://github.com/xseman/pando/commit/e051062ff39c0f965a6b418fbf9d12e956573fb0))

## [0.5.0](https://github.com/xseman/pando/compare/v0.4.0...v0.5.0) (2026-10-09)


### Features

* a new session runs a terminal or an agent profile ([612cec7](https://github.com/xseman/pando/commit/612cec7c0d51a57bf015dff097453ba0787dd3c6))
* a session running a shell command shows as shell ([d544a00](https://github.com/xseman/pando/commit/d544a00d5f573089fbebac85971caae4ab369553))
* **daemon:** session.screen reports the cursor style the app asked for ([b5c170c](https://github.com/xseman/pando/commit/b5c170c281b2dbb2726bf1f9bd213db8a9275985))
* pando builds and runs on Windows ([#16](https://github.com/xseman/pando/issues/16)) ([20368fb](https://github.com/xseman/pando/commit/20368fb61091e0ea3ce4e738d76a9a36b94589c5))
* **ui:** ↑ and ↓ in the message box walk the commit messages ([6554c42](https://github.com/xseman/pando/commit/6554c42a1d6a1fcce34c51dfb45346fdfb0b031a))
* **ui:** a + after a project's or a worktree's name starts a session ([b020a1a](https://github.com/xseman/pando/commit/b020a1a147812895e3762fe27e2135dab771951f))
* **ui:** a click away closes an empty filter ([88c3e3a](https://github.com/xseman/pando/commit/88c3e3a3fa93ae80150b03da75e08ffba8c33ffb))
* **ui:** a click on a session row opens its tab with the news ([aac7c1c](https://github.com/xseman/pando/commit/aac7c1c826c720c6014e3739e1e9d8512e6ed2f1))
* **ui:** a drawer commit shows its whole message on hover and copies it ([e74a32e](https://github.com/xseman/pando/commit/e74a32e4efd86a1048bc8b9abcd6077ed1cf33f5))
* **ui:** a file clicked open takes the preview tab until it is kept ([1784579](https://github.com/xseman/pando/commit/178457963f4576be738d4f73977cd6ac053469c0))
* **ui:** a file's diff shows the whole file with z ([e9111c5](https://github.com/xseman/pando/commit/e9111c5b2df0923949313cbefcd6afb8eb7b5a14))
* **ui:** a new branch's name is sanitized as VS Code does ([e270e88](https://github.com/xseman/pando/commit/e270e88719c6db2a9ac33c6a53ddcd15446d0d3c))
* **ui:** a new session runs an installed agent, never a bare shell ([fd0a4ff](https://github.com/xseman/pando/commit/fd0a4ff5844fd8a7c23e59db132a94a3a4382f9c))
* **ui:** a session's Spaces row counts its tabs ([b2256af](https://github.com/xseman/pando/commit/b2256af096e511f9636bb487cd88eb91db46d784))
* **ui:** a Spaces drag marks the drop with a tinted row between rows ([4740bfd](https://github.com/xseman/pando/commit/4740bfd89086678865d798c0be82f49dca0c4a3f))
* **ui:** a Spaces drag marks where the row lands, moves it on release ([7a7ff17](https://github.com/xseman/pando/commit/7a7ff17d11c0fdb7a6d95aa521f9e499537b525b))
* **ui:** a tab under the mouse tints in hover_bg ([b614371](https://github.com/xseman/pando/commit/b614371d4f28fb5ffb2f4310a2d2bb24d724b2d6))
* **ui:** an unfolded space goes last among the open ones ([3863ceb](https://github.com/xseman/pando/commit/3863ceb795a4fdc7a7f6f9ca983cad2c105f6792))
* **ui:** draw session and terminal tabs like the editor's ([ea0476c](https://github.com/xseman/pando/commit/ea0476c6a7dab86550084621229dfd5a9010ccc4))
* **ui:** give the session's tab strip the buttons and a rule, drop its header ([a233865](https://github.com/xseman/pando/commit/a23386581fbac71ea985344dd9cb188b8117b3d6))
* **ui:** give the Terminal panel's strip the buttons and a rule ([540e6c9](https://github.com/xseman/pando/commit/540e6c9a5c948fb2a870bfbc30cfff561eb8f6cd))
* **ui:** group the Spaces and session context menus by action ([628fffa](https://github.com/xseman/pando/commit/628fffa1b3022cd2d7cc305c56d0e594422c2a73))
* **ui:** label the Spaces hover buttons ([374910f](https://github.com/xseman/pando/commit/374910f6d6ceb132613a5da6a7005d31f9871170))
* **ui:** light the Terminal's sash on the rule under its strip ([8b2577a](https://github.com/xseman/pando/commit/8b2577a9bd107f07dad8886d895bd718a99a4cdf))
* **ui:** list recently opened files first in quick open ([41f8d8c](https://github.com/xseman/pando/commit/41f8d8c5a2be5d54308b85ae24c862591c0f7943))
* **ui:** maximize a session stretched over the editor ([7f6c6fe](https://github.com/xseman/pando/commit/7f6c6feb7a77892b1aa97cf9b0d6f278ec0e4ae7))
* **ui:** maximize the session or the file over the editor area ([d36a69d](https://github.com/xseman/pando/commit/d36a69d2f9020f592594d8d4cd168f2ce0261c3c))
* **ui:** raise the active tab's close button under the mouse ([b603443](https://github.com/xseman/pando/commit/b6034436735bf151a72d7840fc91a975e069f9ad))
* **ui:** sort Explorer files by type, from its menu or explorer_sort ([6571271](https://github.com/xseman/pando/commit/65712712b12c84197b51b4ebb43cfd6844ad5adc))
* **ui:** Source Control drawers stack like Spaces' projects ([0bdfb5b](https://github.com/xseman/pando/commit/0bdfb5b2efcab27e75c1fa1efa16262716d2f254))
* **ui:** Source Control's context menu comes in groups ([405010f](https://github.com/xseman/pando/commit/405010fca4220248f1a9aa2d0fcbe0d62f01223b))
* **ui:** Spaces sorts projects manually or by their last output ([2a7b2b7](https://github.com/xseman/pando/commit/2a7b2b79c8c18bfbaa4a169edc4398dd83bfee0e))
* **ui:** text boxes framed in a thin line, the accent while typing ([84d8f89](https://github.com/xseman/pando/commit/84d8f89bca894abb48bd5bbb4e2b70faca7c7dbc))
* **ui:** the Commit button greys out until something is staged ([d8f8ec1](https://github.com/xseman/pando/commit/d8f8ec1b934490a4d31be05ee0c0848cc8e022de))
* **ui:** the Compare drawer lists the files a commit or a pull request changed ([200ca0a](https://github.com/xseman/pando/commit/200ca0a459125c0cea047fb43f4b375eda251037))
* **ui:** the Compare drawer's ✕ drops the comparison ([23e01fb](https://github.com/xseman/pando/commit/23e01fbf5817773c2bcce513165a62fe5c507cc7))
* **ui:** the cursor is the terminal's, in text boxes and sessions too ([7b26e8b](https://github.com/xseman/pando/commit/7b26e8b03467cb5725b4c80966dc829789425df9))
* **ui:** the message scramble settles once, then only its dots pulse ([6f14f98](https://github.com/xseman/pando/commit/6f14f985e9053a5d88f8a79cc9b027f47814e389))
* **ui:** the Settings modal groups its toggles under headings ([e61ea0c](https://github.com/xseman/pando/commit/e61ea0c192a93df3932b10ef4bbf45bcbf2cb06d))
* **ui:** tint the + button of the session and terminal strips on hover ([dacf6aa](https://github.com/xseman/pando/commit/dacf6aaa31e860e95f6eef92f0546cb2de833e3c))
* **ui:** VS Code style editor tabs, breadcrumbs and buttons in the strip ([3504339](https://github.com/xseman/pando/commit/3504339d37c34110318c1d0d8f68a9a25b5ac513))


### Bug Fixes

* **daemon:** wait for a shell to be listed before handling its exit ([7a58287](https://github.com/xseman/pando/commit/7a58287d335e55b74ee2815e945530c3e5d4be17))
* **ui:** a drawer line's menu leaves out View as Tree and Collapse All ([36d9c18](https://github.com/xseman/pando/commit/36d9c18a57228cb386b0e3bbc24a4c516b64132f))
* **ui:** a selected session row pulses for its tab's new state ([a8485b1](https://github.com/xseman/pando/commit/a8485b1ef7044f81f8c8f5e29affbb2a9dd45a7a))
* **ui:** each project keeps its own Compare drawer comparison ([449fa09](https://github.com/xseman/pando/commit/449fa0973c39a139d62300ff7b4b961ec1034a6e))
* **ui:** key hint columns fit their widest key and description ([4b71e64](https://github.com/xseman/pando/commit/4b71e6432b592555fdf18245079ab7a68f98eb75))
* **ui:** only an agent's tab shows a status glyph ([438cb16](https://github.com/xseman/pando/commit/438cb167bcfe1876d60674ee8c0aa49270721422))
* **ui:** the commit message keeps its color without focus ([fbf9008](https://github.com/xseman/pando/commit/fbf9008cd8810281223337ce687442a4e0ddebde))


### Documentation

* **demo:** re-record the GIFs with the new tabs, strips and breadcrumbs ([f7c29f7](https://github.com/xseman/pando/commit/f7c29f77eef87635f6e5446fa6ea52bed11bf4cf))
* **demo:** run the demo's claude in a config directory of its own ([75cd179](https://github.com/xseman/pando/commit/75cd1793ac14249873f8a79329e95d73df9c27b4))
* **readme:** lay the tour out as a two-column table of GIFs ([fa4c1c3](https://github.com/xseman/pando/commit/fa4c1c3edaf2e180608e0979392872e8fb620faf))
* **readme:** move install section before features ([bb53ef8](https://github.com/xseman/pando/commit/bb53ef82fc43e7d17a10a8f379dee2d7aafcde2c))


### Maintenance

* run make test on macOS ([#17](https://github.com/xseman/pando/issues/17)) ([e0e94f0](https://github.com/xseman/pando/commit/e0e94f0a6ab46e8ec459ebbea151e8a08b248c82))


### Testing

* the e2e session picks a harness of its own ([9c5d84a](https://github.com/xseman/pando/commit/9c5d84aaf7c8299d64794eed27ef570740a44585))

## [0.4.0](https://github.com/xseman/pando/compare/v0.3.0...v0.4.0) (2026-10-02)


### Features

* **daemon:** a shell command under an agent reads running ([d5844e5](https://github.com/xseman/pando/commit/d5844e5d2ae0a41603f72f2b21a8f3326b947e21))
* **daemon:** session.move reorders a session's tabs ([15d5f21](https://github.com/xseman/pando/commit/15d5f215c71f1d0101ee439addc4bae30915f699))
* **scm:** Untracked Changes discards, deleting the files after asking ([f1a9103](https://github.com/xseman/pando/commit/f1a9103fc7f784d111fd9c80ddbd2384f56af2b7))
* **ui:** a faint border parts the activity bar from the view ([04ccf0f](https://github.com/xseman/pando/commit/04ccf0f8409af48f4c12c9cf04e4d3103796a520))
* **ui:** a folded project tops the folded list and unfolds to its place ([1a3c67d](https://github.com/xseman/pando/commit/1a3c67d451c3ec669f3fe8dafd194c6b14cde15c))
* **ui:** a GitHub view where gh is installed ([e73cc6c](https://github.com/xseman/pando/commit/e73cc6c04c2ffc01282dcf79638cd6cc8a987998))
* **ui:** a narrow screen gives the session the editor area and keeps Spaces ([e9c6fe3](https://github.com/xseman/pando/commit/e9c6fe3d9ec7779c596b22645f793993edb5577b))
* **ui:** a pane header that resizes lights as a sash ([ea920b5](https://github.com/xseman/pando/commit/ea920b5df8dabf078d846a96e094231f6a1872f5))
* **ui:** a scrollbar's slider shades under the pointer ([27d17de](https://github.com/xseman/pando/commit/27d17de86d5045815aac6b64cfce9b04f28b2172))
* **ui:** drag a tab along its strip, dropped where a mark shows ([26f515c](https://github.com/xseman/pando/commit/26f515c601eaf93e7271750094e5ed40f3c63cda))
* **ui:** folded projects sit on the Spaces panel's bottom edge ([c42ba1d](https://github.com/xseman/pando/commit/c42ba1d1d498234555455038f45551efd17488da))
* **ui:** folded Spaces projects stay folded across restarts ([988afee](https://github.com/xseman/pando/commit/988afeedee0d04fa5fac9a96b0a9c3c0b3650dda))
* **ui:** Move Tab Left / Right, for editors, session tabs and shells ([8bf7d52](https://github.com/xseman/pando/commit/8bf7d52e35ac79fb26950bf5a02575cabf515e3f))
* **ui:** questions ask with a row of buttons, as VS Code's dialog ([9a940f4](https://github.com/xseman/pando/commit/9a940f4261371e650f9599d90ea568925ac90dd4))
* **ui:** Sort Lines Ascending sorts the selected lines, or the file ([ae2fa3c](https://github.com/xseman/pando/commit/ae2fa3c24e9d514e24621b838b3949d466a2e6a0))
* **ui:** the active tab stands out of its strip ([8bf2ce6](https://github.com/xseman/pando/commit/8bf2ce658fc9ebe72d299ac9828bacae954badc1))
* **ui:** the branch picker groups its matches, the best one first ([232a8fc](https://github.com/xseman/pando/commit/232a8fc4da84783e518ebcd595542daa24b196aa))
* **ui:** the open view's icon hides its sidebar, leaving the activity bar ([baaff89](https://github.com/xseman/pando/commit/baaff89fcfeb4b1e2ed5dbad6a8640ea4ca9fb04))


### Bug Fixes

* **daemon:** a background shell under an attached claude job reads running ([e3ff97e](https://github.com/xseman/pando/commit/e3ff97ea3e1233c48d560707da6101986a5967a0))
* **daemon:** a claude permission prompt in a narrow column reads blocked ([37dd157](https://github.com/xseman/pando/commit/37dd1577667fc8b7aa4287ab9502bee54d907b05))
* **daemon:** a claude session with a live subagent reads running ([18a08d1](https://github.com/xseman/pando/commit/18a08d1e47afff89a0b581b17709bd099bb2ac7f))
* **daemon:** a worktree a gone claude locked can be deleted ([0a8d1f3](https://github.com/xseman/pando/commit/0a8d1f3eeae0e7a4a4bd1eceb6d4d1ca1f904cf3))
* **daemon:** an agent run as its session resumes in place of a fresh one ([3358680](https://github.com/xseman/pando/commit/3358680a827199645c5b5f7de0958a0049902196))
* **daemon:** session input reaches the app in the order it was sent ([8e93c3b](https://github.com/xseman/pando/commit/8e93c3ba67e9dc9cc60fb23c4e3bcb4ce4cbd1d9))
* **ui:** a second click on a session in Spaces leaves no row selected ([eaa9a22](https://github.com/xseman/pando/commit/eaa9a2210ff22af18f33a650a9309b076b8351ba))
* **ui:** ctrl+v pastes into a find box and every other text box ([c64ecfe](https://github.com/xseman/pando/commit/c64ecfe22415f27a7287ff83e6aebe0ed0525346))
* **ui:** pgup and pgdn page a terminal's scrollback ([00c59df](https://github.com/xseman/pando/commit/00c59dfafbee725aea4ca565375e73762f35c850))
* **ui:** pgup and pgdn page the Spaces and Search lists ([35528b2](https://github.com/xseman/pando/commit/35528b28e93c25f693b5ca1eae5a5e58ed74a6e8))
* **ui:** the help lists alt+, alt+. for back and forward ([bd3b678](https://github.com/xseman/pando/commit/bd3b6786788967858d7377aa442459186847e8ca))
* **ui:** the Spaces selection stays on its session when the list re-sorts ([7b49a3f](https://github.com/xseman/pando/commit/7b49a3fb1ba68c08e180ead82e5f6ef7e3cd1443))


### Documentation

* a shorter README and a user guide, every doc terse with diagrams ([c3b0cd0](https://github.com/xseman/pando/commit/c3b0cd07a5dd5a2272183c0c0711b390efdf7964))
* **demo:** re-record the GIFs with every panel on the left ([73a8f10](https://github.com/xseman/pando/commit/73a8f10099231ec6a0f12d7a9f18f2f7e3daf58a))
* **demo:** record the GIFs light in a blue gradient, with a README still ([09bfdae](https://github.com/xseman/pando/commit/09bfdae4229ff8caea14cc6602b3ea19e6efde45))
* re-cut the demo GIFs around what pando is for ([dd87f64](https://github.com/xseman/pando/commit/dd87f64d6a216f1980af3865dc24816fd23d2615))
* **skill:** pass a message to another session, after proving the target ([71b8402](https://github.com/xseman/pando/commit/71b84020cc089e309ada672195571d800d18037a))
* trim the README, fix what went stale in docs/ ([e6fbfc8](https://github.com/xseman/pando/commit/e6fbfc8bcd9739526de2123c05ccf6e43f44011c))

## [0.3.0](https://github.com/xseman/pando/compare/v0.2.0...v0.3.0) (2026-09-27)


### Features

* claude resumes as a background session, in its own config dir ([569d3cd](https://github.com/xseman/pando/commit/569d3cd8390805ee9f5d697a2a28ac0bae5f8225))
* **config:** word_wrap setting ([fba3d7d](https://github.com/xseman/pando/commit/fba3d7dc282af9ff79a4dd28978806686b3a6e23))
* **daemon:** icons default to ascii ([2079022](https://github.com/xseman/pando/commit/2079022e88ed54d5fdb51abbc4d08907d21292ca))
* every worktree keeps its own session column ([cff757b](https://github.com/xseman/pando/commit/cff757ba21d9993398965f2eb56b26be8a272bd3))
* **ui:** a margin left of a file's line numbers ([d17300a](https://github.com/xseman/pando/commit/d17300abfe1cc4bc5597b56aa77ec2f2df571b76))
* **ui:** a session column dragged nearly off the screen closes ([f4a50ce](https://github.com/xseman/pando/commit/f4a50ce05b6f0d9ed3d40704daaeeb028fe0ec56))
* **ui:** cut, copy and paste files in Explorer ([867df4f](https://github.com/xseman/pando/commit/867df4f5bf2a3733a46e1aa7bab4bf0fac1d0b6c))
* **ui:** double click selects a word, triple click the line ([b40ec20](https://github.com/xseman/pando/commit/b40ec20569b2510f7454f8b381b24a2d434a2169))
* **ui:** drag a session within its worktree ([ff20cde](https://github.com/xseman/pando/commit/ff20cdee6976815ef37ddceea1500d8a041b69d9))
* **ui:** drag a worktree within its project ([a1e5701](https://github.com/xseman/pando/commit/a1e570143a34aa198e4554f05246b6c5d96bc03c))
* **ui:** editor_limit closes the least recently used tab ([a2b469b](https://github.com/xseman/pando/commit/a2b469b005132268ebf3e7aea05886269d59f0a8))
* **ui:** editor.changeIndentation opens the Tab Size menu ([76ab8d8](https://github.com/xseman/pando/commit/76ab8d8bb814d29f1bbeae99f545f88b7bedf838))
* **ui:** find in a session's or the Terminal's scrollback ([2c45c10](https://github.com/xseman/pando/commit/2c45c10f504df3040da11cf608760e4566d14adf))
* **ui:** horizontal scrollbar and a word wrap toggle in the editor ([f7a0e12](https://github.com/xseman/pando/commit/f7a0e12e7c39d163754353dd491a343af95e343b))
* **ui:** light a divider under a resting pointer ([3c96500](https://github.com/xseman/pando/commit/3c965009fb0aa24811e7bfa1657123ad8f00744b))
* **ui:** markdown_width caps rendered Markdown at 80 cells ([a470d5e](https://github.com/xseman/pando/commit/a470d5e14741eda4601e0eef26b39c7fb5140066))
* **ui:** render_whitespace draws spaces and tabs ([69652bc](https://github.com/xseman/pando/commit/69652bc0151efee3cf1bcf5f95b466e2005b85f4))
* **ui:** rendered Markdown in glow's layout ([95c6bec](https://github.com/xseman/pando/commit/95c6bec2924d1c50821729bac421ee5e8249b07b))
* **ui:** sidebar list scrollbars drag as the editor's does ([28e25af](https://github.com/xseman/pando/commit/28e25af1382144fc8bef0df16b4c81ba186aea1c))
* **ui:** source control tints only the status letter, folders get a dot ([cdd322b](https://github.com/xseman/pando/commit/cdd322b239ce4847fb1d17c101fb2691fd58019a))
* **ui:** subtler line numbers, the cursor's line lit ([25e239d](https://github.com/xseman/pando/commit/25e239d05cfbd22315bf3a07a1cd0c9b368362e8))
* **ui:** text effects for names, counts and busy labels ([cc329a7](https://github.com/xseman/pando/commit/cc329a70c6d6d79b0ea1fd53aa43444e690b4c81))
* **ui:** the status bar shows Ln, Col and sets the tab size ([7e62ba3](https://github.com/xseman/pando/commit/7e62ba366b89676e135001a9065d58df4711aae5))
* **ui:** VS Code's context menu in Explorer ([46cda74](https://github.com/xseman/pando/commit/46cda7498f65287356a9ecbb1374079769bbdf96))


### Bug Fixes

* **daemon:** a restart attaches to a background job it read half written ([03cf08c](https://github.com/xseman/pando/commit/03cf08c8476ce281bc503949b20a7dba5716ca7a))
* **daemon:** an attached session is named after its conversation ([a6a0c77](https://github.com/xseman/pando/commit/a6a0c7724aa977114c5c517d139c939356abbb28))
* **ui:** a Commit button with nothing to do is grey, not tinted ([ef4cc79](https://github.com/xseman/pando/commit/ef4cc7934cd2aaf62fa80e99668524cf873690a9))
* **ui:** keep a terminal selection on its text as it scrolls ([cd374f4](https://github.com/xseman/pando/commit/cd374f437546ddfa392825c52a17a4f99ebc199c))
* **ui:** keep the Terminal panel open or shut per worktree ([047e683](https://github.com/xseman/pando/commit/047e683bbd54ba19b9b86074e3800fec9b497cf6))
* **ui:** leave ctrl+j to a session or a shell ([dbcb503](https://github.com/xseman/pando/commit/dbcb50316dd1fdcc52bb154b6a5a2ed7ad9c71d6))


### Documentation

* re-record the markdown, edit, vim and tui demos ([bec15ef](https://github.com/xseman/pando/commit/bec15efd8f4e018d5d704dd70eb74c89e2871e95))
* the icons need only Nerd Font glyphs, not a patched font ([e1e34cd](https://github.com/xseman/pando/commit/e1e34cd56e3125ed48b1ad01ce6df271a5497aa2))


### Testing

* **daemon:** give waitFor 15 s on a slow runner ([526f94b](https://github.com/xseman/pando/commit/526f94be5618085c97626952a1356b74224e4ec1))

## [0.2.0](https://github.com/xseman/pando/compare/v0.1.0...v0.2.0) (2026-09-23)


### Features

* **daemon:** attach to background jobs and keep a session's claude config ([7735b9a](https://github.com/xseman/pando/commit/7735b9a402c90432b1f119f3bb558ca8556c5282))
* **daemon:** resume each session's own conversation ([fb550e1](https://github.com/xseman/pando/commit/fb550e147f39efcc6fee29c1c04510cd47afefc4))
* give every session its own terminal tabs ([b0aea6c](https://github.com/xseman/pando/commit/b0aea6c130067d55b817de887495fcfdec62068a))
* name new worktrees at random, as herdr does ([7522571](https://github.com/xseman/pando/commit/752257158be00d03a43912cfd99f74ac5a057cbe))
* open every terminal in one configurable shell, falling back on bash ([da59ea7](https://github.com/xseman/pando/commit/da59ea7c3edffb5a8a0310426f0434a370259883))
* **scm:** continue a merge with git's prepared message ([d4c0920](https://github.com/xseman/pando/commit/d4c092010d92b5f85d06a54c209699fa596eb9cb))
* **ui:** add VS Code's scrollbar to the editor and terminals ([ee92c13](https://github.com/xseman/pando/commit/ee92c13b575315c4a94b0ada1f2c47a45b39137c))
* **ui:** filter, sort and group Spaces sessions from a view menu ([b3aab72](https://github.com/xseman/pando/commit/b3aab72ad39a1e9f274de31cfc60cb7504b93c46))
* **ui:** frame the tabs of every strip ([4b99378](https://github.com/xseman/pando/commit/4b99378e7b0612247a82e8bf1041c490b3eb19fa))
* **ui:** give every session tabs of its own, as herdr does ([87b4920](https://github.com/xseman/pando/commit/87b4920385c8f5a06ebb69bc22b732181a90b5ed))
* **ui:** keep session tabs inside their space ([6e7062c](https://github.com/xseman/pando/commit/6e7062ce8dd83f96ec61e46c646dfc589b8864f7))
* **ui:** let the focused part own its keys, as VS Code's when clauses do ([4d49dec](https://github.com/xseman/pando/commit/4d49dec6b8e39fceb7d891be70c1f646ae999464))
* **ui:** multi-line commit message box with generate menu ([8488e53](https://github.com/xseman/pando/commit/8488e53e5eaddedb8c6c4f99641e3f7ed7554cbf))
* **ui:** pulse a session's tint until it is clicked ([3a98759](https://github.com/xseman/pando/commit/3a98759e08b2cabcc123737919a250ecabf82553))
* **ui:** select and copy text in terminals by dragging ([f3be4cc](https://github.com/xseman/pando/commit/f3be4cc1747ca82d52088172810686ec9e42cc8e))
* **ui:** split Commit button hover and match the message box's ∨ to it ([50c7eef](https://github.com/xseman/pando/commit/50c7eefb5285a6de800e59042b321250df64ef06))
* **ui:** tell a project's checkout from its worktrees in Spaces ([c983061](https://github.com/xseman/pando/commit/c9830619d4d44094f7e4dbcf149acff811010bc5))
* **ui:** tint sessions that wait or finished unseen ([a391031](https://github.com/xseman/pando/commit/a39103191ced6e366f20408d356ecce5bab9a54b))


### Bug Fixes

* **daemon:** resolve session names in focus ([325cc4d](https://github.com/xseman/pando/commit/325cc4d8ac874792629d672860114a60d94fefc0))
* **daemon:** send modified special keys to the session ([080dff4](https://github.com/xseman/pando/commit/080dff4dae67d5cc9eec3a458efee68c12b828d9))
* **daemon:** stop an agent that lost its terminal before resuming ([0426bc7](https://github.com/xseman/pando/commit/0426bc7daa3316ca22abc298d10db76b790447c3))
* leave the scrollbar and the wheel to an app on the alternate screen ([8bc089b](https://github.com/xseman/pando/commit/8bc089b628b8d4fbd2498d7fb8c8908e86cc5d65))
* **ui:** attach the terminal panel to its shell at start ([9d68885](https://github.com/xseman/pando/commit/9d6888568b23663d0f987ff32054ea6e594dd8ea))
* **ui:** copy to the desktop's clipboard, not only through OSC 52 ([83a9bd9](https://github.com/xseman/pando/commit/83a9bd9251ce6ca8c881789c9efdb59f244e633c))
* **ui:** keep a tab's width when it becomes active ([15c034e](https://github.com/xseman/pando/commit/15c034ec1227f36bb40322625d19bbfb8f8d8565))
* **ui:** keep the active tab on a narrow session strip ([6c629a6](https://github.com/xseman/pando/commit/6c629a61df16beb74e8819ca3ed645dceecdd6cb))
* **ui:** keep the hover under a menu where the right click left it ([4713395](https://github.com/xseman/pando/commit/4713395eb44698f947cab30421c478779f9b6cc1))
* **ui:** paste into a session docked in its own column ([e9d1d08](https://github.com/xseman/pando/commit/e9d1d084b2bfa50d4ae60649e13b6fb81d7da750))
* **ui:** space out Source Control row buttons and widen their hover ([368485c](https://github.com/xseman/pando/commit/368485c0b2e17d6da806242ce0519aa61a18bd58))


### Documentation

* re-record diff, lsp and tui demos ([698102a](https://github.com/xseman/pando/commit/698102a76eb03b5e538582165891ab2d71f26de0))
* re-record the demos with the features since v0.1.0 ([ede5069](https://github.com/xseman/pando/commit/ede506936473cac85d9ce9aa5d48ef66efb5da8e))

## 0.1.0 (2026-09-19)


### Features

* terminal workspace for AI agent sessions ([c3e926c](https://github.com/xseman/pando/commit/c3e926c646dfe4b277fc5e43f1bb4c623c404111))
