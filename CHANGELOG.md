# Changelog

## [0.13.0](https://github.com/xseman/pando/compare/v0.12.0...v0.13.0) (2026-11-13)


### Features

* **cli:** session profile moves a session by profile name ([1434724](https://github.com/xseman/pando/commit/14347244c055f8f6115d9ffa7120c1fe4c961356))
* **daemon:** model and effort of a claude session and its subagents ([1fda86e](https://github.com/xseman/pando/commit/1fda86e19f47234d4e6903e927fa646ed5c33d0a))
* **daemon:** session.profile by profile name, with text to submit ([5870e05](https://github.com/xseman/pando/commit/5870e0540f6ec5e685c9bc98185c7f53501dcb3e))
* **ui:** a session's and a subagent's tip names its model and effort ([90d095e](https://github.com/xseman/pando/commit/90d095ecbe05a2f0bcfe2e3640c3406435553e91))
* **ui:** profile picker in columns, Usage meters in blocks ([1c02eb0](https://github.com/xseman/pando/commit/1c02eb03fc8774d2c4862d4076d71a63e9b7a2bb))


### Bug Fixes

* **cli:** help lists session.profile and the usage methods ([6db5e8e](https://github.com/xseman/pando/commit/6db5e8ebc4c15a6aa0e0c8cdb4383ad5bb6e297f))
* **ui:** indent guides no longer freeze on a deeply indented line ([d5d2fc4](https://github.com/xseman/pando/commit/d5d2fc4bc78c5f950c1cda4022d82719c1f9ee8a))

## [0.12.0](https://github.com/xseman/pando/compare/v0.11.0...v0.12.0) (2026-11-12)


### Features

* **daemon:** an agent's other configs found by its account file ([58ec0ec](https://github.com/xseman/pando/commit/58ec0eca666829431f7df54d63df06e54e4bd10f))
* **ui:** Usage tab spaced out, agent marks and the cursor on an account ([25e7f3d](https://github.com/xseman/pando/commit/25e7f3d97de4fa34ccf3a431ad0c61f4fcd81274))


### Bug Fixes

* **daemon:** configs found in a home that is a link or named as a skipped directory ([2042ea1](https://github.com/xseman/pando/commit/2042ea17061c586d14ffd4c20b36663156935025))
* **daemon:** no note for a busy usage service ([abf68e1](https://github.com/xseman/pando/commit/abf68e13ab0d3294ebc7ad7cfd7b725de532c8be))
* **ui:** Usage limits back under their account's name ([37ec399](https://github.com/xseman/pando/commit/37ec399c7a4b5fd7748f5ccdcd526da8803b1eb6))

## [0.11.0](https://github.com/xseman/pando/compare/v0.10.0...v0.11.0) (2026-11-11)


### Features

* **daemon:** plan limits by sign-in and profile switching ([c07417a](https://github.com/xseman/pando/commit/c07417a214f0a54dfc910e43323db483078a06fb))
* **git:** choose the agent and style that write commit messages ([daa5c43](https://github.com/xseman/pando/commit/daa5c43381380753804396caa65631a384b6aab4))
* **ui:** a session names the profile it runs in ([9d43ae3](https://github.com/xseman/pando/commit/9d43ae332b7316ec1f29ea2b55ce14ec67ecaca8))
* **ui:** a tip naming the model behind a Usage meter's mark ([250db86](https://github.com/xseman/pando/commit/250db86bdcfb3fb1bb67b5ae0699aae99b3e4e8b))
* **ui:** a tip on an account's use in Usage ([1d44380](https://github.com/xseman/pando/commit/1d44380bff7d3a3d1395be28113d38aefe0c57bb))
* **ui:** Add Project first in Open Project ([e82c2d6](https://github.com/xseman/pando/commit/e82c2d684ff77b1f7d56fca0c06f42782127e19c))
* **ui:** header tabs light under the mouse ([194ead5](https://github.com/xseman/pando/commit/194ead53425aad9f095fd26dad32b864262fc974))
* **ui:** home, end, pgup and pgdown in pickers ([a9d1a04](https://github.com/xseman/pando/commit/a9d1a041d56ccd0742d368488a0364c31a2b4f9f))
* **ui:** open pickers and dialogs in the middle over a dimmed frame ([b305973](https://github.com/xseman/pando/commit/b305973fcaffc39939022509035531d0f0f5f4ad))
* **ui:** opening a row found in Sessions clears its filter ([c967d66](https://github.com/xseman/pando/commit/c967d6632ba735be12f8a709f857fc09f662bcf5))
* **ui:** pinned sessions show their branch ([1bcc7cf](https://github.com/xseman/pando/commit/1bcc7cf961b7fe0294e7751ea3daf32dc5c2407b))
* **ui:** quick open lists changed files, lines by file and commands ([01e1289](https://github.com/xseman/pando/commit/01e1289ce5e29bdde46f27e86f36cbd82fad6da1))
* **ui:** saved prompts on alt+i ([6b6ac0d](https://github.com/xseman/pando/commit/6b6ac0d0306943f32e890ab4216ce732114ea128))
* **ui:** Usage is the tab after Sessions ([8e25e23](https://github.com/xseman/pando/commit/8e25e2311d4dcd9c02e07a422fd1a0f3169d1461))
* **ui:** Usage tab with plan limits and Move to Profile ([a37efd2](https://github.com/xseman/pando/commit/a37efd2f0ccbe83b3e420902ca7b519ad2645961))


### Bug Fixes

* **claude:** run in the foreground where --bg finds the workspace untrusted ([7de4c9e](https://github.com/xseman/pando/commit/7de4c9e3cc9e0fdd44cb41bd8cd744ac79340aa5))
* **daemon:** a cancelled resume picker closes its session ([1ea4dfe](https://github.com/xseman/pando/commit/1ea4dfe92e19fff763115f17a4979a6a144e8ad0))
* **daemon:** an agent suspended with ctrl+z comes back ([d67b78d](https://github.com/xseman/pando/commit/d67b78d603ec33a119f79dff4cf401039f6bb752))
* **daemon:** no unknown key warning for the left and right columns ([b54347c](https://github.com/xseman/pando/commit/b54347c9c60a40aa597e2841cac8e9c5c805a177))
* **daemon:** tell subscribers an asleep session moved profile ([bc440f3](https://github.com/xseman/pando/commit/bc440f38ca8e33c03a593cd03943c26af8edd03c))
* **ui:** an empty file opens ready to edit ([ae5279d](https://github.com/xseman/pando/commit/ae5279d358499453ed5ca0c5a890ca4b03716133))
* **ui:** quick open keeps its tabs whole when Text's count does not fit ([894caa8](https://github.com/xseman/pando/commit/894caa87a046ae1a2be0dc425249de6bf5270eae))
* **ui:** quick open's All finds a project with no session ([6a9b21d](https://github.com/xseman/pando/commit/6a9b21dc88ea7b8d682130af96c015b923ba304c))


### Performance Improvements

* **ui:** draw modals and quick open several times faster ([f79ad16](https://github.com/xseman/pando/commit/f79ad1619939ec4756467b627b2ab5618a71a0d2))


### Documentation

* **demo:** re-record every GIF and the hero ([f32c3b6](https://github.com/xseman/pando/commit/f32c3b63c58d4bac355c956e177442b71329e2bd))
* **demo:** Usage and quick open GIFs, the merge through Merge Branch… ([a5d0ba2](https://github.com/xseman/pando/commit/a5d0ba239ede68b7fd701521774bacd7f847346f))
* **readme:** quick start after install, development and license ([271964d](https://github.com/xseman/pando/commit/271964de3174ebb7bdc58c432b5a714659cddd07))
* rename CLAUDE.md to AGENTS.md ([983c802](https://github.com/xseman/pando/commit/983c8022acd8989e631741ccabb940a3f3a3437d))
* what moving a session to another profile asks and costs ([e47c522](https://github.com/xseman/pando/commit/e47c522d75323d28289438ae3fbc339bf0bd56d8))


### Maintenance

* **proto:** ConfigDir moves from daemon to proto ([6458489](https://github.com/xseman/pando/commit/6458489aa74465434aa80ac83c331810d770aad2))

## [0.10.0](https://github.com/xseman/pando/compare/v0.9.0...v0.10.0) (2026-11-06)


### Features

* **ui:** ↓ from the folder picker's path lands on the first folder, past .. ([a5ea2e2](https://github.com/xseman/pando/commit/a5ea2e2ace4784c8f6d88001ef0a5c8edb30264d))
* **ui:** ⏎ on the folder picker's path goes into the folder it names ([e9ae381](https://github.com/xseman/pando/commit/e9ae38117d2577df9f0977eb3d33034ed00d58b6))
* **ui:** a binary file's card opens it in the default app ([71fb238](https://github.com/xseman/pando/commit/71fb2384033bdb1b45c4209d1c3a39ee08b3b112))
* **ui:** a quiet tail under the Sessions tree ([abeafad](https://github.com/xseman/pando/commit/abeafad55124db16926c391db26dfdf0c835f9cb))
* **ui:** a session going to work opens its folded project ([2cae493](https://github.com/xseman/pando/commit/2cae49386c23f9fd7549475ff00be721fa986924))
* **ui:** Abort Merge while a merge is in progress ([3c6393e](https://github.com/xseman/pando/commit/3c6393ebe1af087fd4e31b649e8954fcff495528))
* **ui:** Checkout to… and VS Code's branch commands in the palette ([52d4de2](https://github.com/xseman/pando/commit/52d4de2696de965afb68b0f14afa12ad8e386bcc))
* **ui:** each worktree keeps its panel's tab ([c80fab5](https://github.com/xseman/pando/commit/c80fab5f8b5ed021b61af43371d12c08efc3024a))
* **ui:** folder picker completes the path on tab, ↑ back to it, ← → across columns ([a5bf087](https://github.com/xseman/pando/commit/a5bf087d19eed6e4e2fbbeb9cf5cf20e375d317d))
* **ui:** Merge Branch…, as VS Code's Merge… ([479ccd8](https://github.com/xseman/pando/commit/479ccd89d8805f9f14f9e7d3cce8b4fe30f01e87))
* **ui:** Other Projects lists the three used last first ([a5222e8](https://github.com/xseman/pando/commit/a5222e8a922ea784747f7661a8661172c4dd15f9))
* **ui:** pinned sessions grouped under their project ([244856c](https://github.com/xseman/pando/commit/244856c29a95b5536dd9d48eb54ece89833f0751))
* **ui:** quick open's All, Files, Sessions and Text tabs ([d0ec511](https://github.com/xseman/pando/commit/d0ec511adf56ddcc85fe67da6ba8564a5a385768))
* **ui:** quick open's Sessions tab is the Sessions tree ([38dd3e6](https://github.com/xseman/pando/commit/38dd3e65a00076a46b7643c1fefb600f6c12c4dd))
* **ui:** Rename Branch… and Delete Branch…, as VS Code's ([38d755d](https://github.com/xseman/pando/commit/38d755dbb3f83dec69d00e391514c8fb65147f07))
* **ui:** Sessions lists what is awake on top, the rest A–Z ([e430347](https://github.com/xseman/pando/commit/e43034715b6bdc6cbe6cf5e1fd555910743f4c57))
* **ui:** Sessions pins a whole project under Pinned ([3bfd5c6](https://github.com/xseman/pando/commit/3bfd5c64e718ab2084b4a5c7e4b4b48ac952b8d7))
* **ui:** Sessions' filter finds words, without accents, by folder ([16c321d](https://github.com/xseman/pando/commit/16c321dfbf3c7e78c5204af5af12e3b9e8b94cd6))
* **ui:** the three projects used last under their own Recent caption ([07f4c1d](https://github.com/xseman/pando/commit/07f4c1dcd35ce687fa3ed1adedb44ba08d953df2))


### Bug Fixes

* **daemon:** a claude scrolled back to the bottom no longer stays running ([72e5edf](https://github.com/xseman/pando/commit/72e5edf5348a82dc4f7a377fda0f481df362d8e6))
* **daemon:** a session killed right after it started stays killed ([a1e0adf](https://github.com/xseman/pando/commit/a1e0adfca3073d2bd8e02bbd6ebd944d097e496a))
* **daemon:** claude's quoted phrases no longer set a session's status ([711dffa](https://github.com/xseman/pando/commit/711dffa2bee790aabf9e7809f81be351a1885b89))
* **daemon:** kill and stop no longer hang on a job holding the terminal ([af99d8e](https://github.com/xseman/pando/commit/af99d8e7a55cac8b1fb0b092530fd63eb4ad3aae))
* **daemon:** session.wait ends with its caller and with its session ([7ea03bd](https://github.com/xseman/pando/commit/7ea03bdcf6c0957c15150cb4b3a73e6d063f8f83))
* **daemon:** shell only while a command alone keeps a session running ([c0f413c](https://github.com/xseman/pando/commit/c0f413c97a38785cd23c6836bf917f93f460983a))
* **daemon:** the detached-job kill test builds on Windows ([55f5d17](https://github.com/xseman/pando/commit/55f5d17a70acb1aac1b7d04754da204026293dbc))
* **git:** git speaks English in any locale ([7f99c9f](https://github.com/xseman/pando/commit/7f99c9f64a5467c1835d46b421791e60654f0d1e))
* **ui:** a closed language server's late reports are dropped ([48de916](https://github.com/xseman/pando/commit/48de916787a799985705bf20107f60877f0dd76d))
* **ui:** a folded project with no session goes on top when switched into ([86ae9f3](https://github.com/xseman/pando/commit/86ae9f3569440cf548e53682d15ac2d06acb32e2))
* **ui:** a paste into a picker runs its change as typing does ([363db52](https://github.com/xseman/pando/commit/363db52dd0c8b4881f2b154fdfd726141ea57b54))
* **ui:** a picked project stays on top until its pick expires ([6931c2c](https://github.com/xseman/pando/commit/6931c2cfbfbdba62aac7224db416947b41752a92))
* **ui:** a picker row cut short keeps a blank before its hint ([9c0aea7](https://github.com/xseman/pando/commit/9c0aea762cf48ef866da03ad111bff79abf48054))
* **ui:** a project picked on top draws as active ([eb6c255](https://github.com/xseman/pando/commit/eb6c255cb576d41c782eda6ac3f5e88e981f110e))
* **ui:** a project whose last session closes stays on top ([ae2a3e8](https://github.com/xseman/pando/commit/ae2a3e87bea72ee8bf0b191bbde48511b969e5bb))
* **ui:** a switch into Recent or Other Projects shuts that caption ([f52d927](https://github.com/xseman/pando/commit/f52d927970c8d87927d40d574bbee66ec51df316))
* **ui:** focusing the empty main area keeps the welcome text in place ([38b9def](https://github.com/xseman/pando/commit/38b9def088985d553f1c33714ee84f5f86ef4ebd))
* **ui:** Go Back brings no discarded edits back and keeps no clean files ([94c6db3](https://github.com/xseman/pando/commit/94c6db38efdcd017c61db319bccf44c78d91e4f4))
* **ui:** keep pinned projects on top of the Sessions tree ([f06827a](https://github.com/xseman/pando/commit/f06827a0a1e22ce05748c5be105da50e466e3155))
* **ui:** no blank row between folded Recent and Other Projects ([7984c10](https://github.com/xseman/pando/commit/7984c107dd89db7514286059d648f7c84ac28958))
* **ui:** no zombie left per sound, opened link or crashed server ([ce7b245](https://github.com/xseman/pando/commit/ce7b245e4e58907ca756654b1580ce480ed0d12a))


### Performance Improvements

* **daemon:** an asleep session keeps a screen for its notice alone ([6e8e6ee](https://github.com/xseman/pando/commit/6e8e6ee7df9ff3381ba7d7e980f9e71f5454fd6c))
* **daemon:** scrollback rendered from the screen, parser buffer 64 KiB ([554a7a1](https://github.com/xseman/pando/commit/554a7a18f598d67de89280038af937996b805a87))
* **ui:** editor tabs laid out once a frame, no copy of every editor ([c5dbf73](https://github.com/xseman/pando/commit/c5dbf73d8ba9ddb7bc6211d97762c8f761591be3))
* **ui:** git refreshes back off while idle, files read on a change ([3c46d9b](https://github.com/xseman/pando/commit/3c46d9b22576422355a4c51e706082756c0c2caa))
* **ui:** Source Control drawers reload on a git change, not every tick ([25cd31f](https://github.com/xseman/pando/commit/25cd31f82353bd9ca1f39cafc9a3735376d8704b))


### Documentation

* **views:** describe pinned sessions under their project ([1ed0ae9](https://github.com/xseman/pando/commit/1ed0ae9d6b452bfda1cadcbdfd0a93c4992ee068))
* **views:** wrap the Other Projects paragraph ([1ebc1ab](https://github.com/xseman/pando/commit/1ebc1ab132ffc218b9818302ccecebe3c79303a1))


### Build System

* **deps:** ultraviolet and vt from forks until upstream takes them ([7ececfd](https://github.com/xseman/pando/commit/7ececfd56165d2c415a2603445f849cb8e398b2f))


### Maintenance

* **ui:** Search's background run and a match's column, for reuse ([e366ee0](https://github.com/xseman/pando/commit/e366ee0ba66029153329b4bb631302905bebb436))
* **ui:** Sessions builds a project's rows in one place ([4b1147a](https://github.com/xseman/pando/commit/4b1147aacf70f4bfd5ce711f1677586fc55aff45))


### Testing

* **ui:** the status bar's project switch shuts No Sessions it came from ([65569e8](https://github.com/xseman/pando/commit/65569e8be3d151fdb80ff7afb2b5079f98a1aeeb))

## [0.9.0](https://github.com/xseman/pando/compare/v0.8.0...v0.9.0) (2026-11-01)


### Features

* **cli:** session send and keys wake an asleep session ([bc51e1b](https://github.com/xseman/pando/commit/bc51e1b449cd2fef80c995865d514b79d1e303b3))
* **daemon:** a worktree's shells move among each other, whatever their parent ([0844ecc](https://github.com/xseman/pando/commit/0844eccd66af668b6d69aa7ff60446ecb8a9ff49))
* **daemon:** sessions sleep after an hour by default ([c16104b](https://github.com/xseman/pando/commit/c16104b5bf49aa400325524c3b08d61b30d10632))
* **ui:** a + after the editor tabs opens a file or a session ([dfcb69f](https://github.com/xseman/pando/commit/dfcb69ff3f8fc4ae22ba63beed503f39f32127a1))
* **ui:** a click on any Spaces project's name switches to it ([5cbeccc](https://github.com/xseman/pando/commit/5cbeccc21926eb4a3fbab28641dbf2208c16b5e2))
* **ui:** a Customizations tab beside Sessions ([3c38b76](https://github.com/xseman/pando/commit/3c38b76af727dff5159acbd73c7e4d842483c7ac))
* **ui:** a double click past the tabs shows the welcome screen ([38cc6ac](https://github.com/xseman/pando/commit/38cc6acfb62a35afaadb232ac359be94b73d1007))
* **ui:** a Spaces project's chevron under the pointer is raised as a button ([43a58f1](https://github.com/xseman/pando/commit/43a58f1cc85644701cea3f3613ba90dd8c980979))
* **ui:** a tab held at the bottom's middle opens a row under all groups ([d0af04d](https://github.com/xseman/pando/commit/d0af04df7ca27a6d358b738b1c3704c462eff838))
* **ui:** a worktree's sessions share its Terminal panel's shells ([92c6216](https://github.com/xseman/pando/commit/92c6216c00ad7d90076991af3510e1c0cdf96c0d))
* **ui:** alt+a goes to the next session waiting on you ([3eab59f](https://github.com/xseman/pando/commit/3eab59f9760e8901cec7a000221ac9a456384182))
* **ui:** alt+n opens the + beside the tabs, and the palette has it ([16c60d7](https://github.com/xseman/pando/commit/16c60d7920d124ddf1e3c3f33ac9904041389a62))
* **ui:** ctrl+shift+d deletes the line in the editor ([484ff9f](https://github.com/xseman/pando/commit/484ff9f799a30b629811ae543d6dccd7a4a01bed))
* **ui:** Resume Session in the + menu ([7f4bdd2](https://github.com/xseman/pando/commit/7f4bdd251d63dc2aff60ecc126b859cb4fef1fb1))
* **ui:** Spaces is called Sessions ([90fb580](https://github.com/xseman/pando/commit/90fb580205a353feecb7d26b182dc9551cfbd4a7))
* **ui:** the drawers sit under a TOOLS strip, as NO SESSIONS in Spaces ([73cc994](https://github.com/xseman/pando/commit/73cc9946c8a33ce20170997f6478b54d7972bc52))
* **ui:** the empty area has the +, alt+s starts a session anywhere ([5f38a7f](https://github.com/xseman/pando/commit/5f38a7f65a87a4fd4b0092df863bb8fb36f25c8a))
* **ui:** the focused group's content is framed on every side ([f858819](https://github.com/xseman/pando/commit/f8588190c10b542c862a4979bf5be2afb7758d4f))
* **ui:** the focused group's frame is drawn heavy ([7d6c151](https://github.com/xseman/pando/commit/7d6c1516ebc80ef951cc7efa3b81bd4330e45918))
* **ui:** the panel is framed apart and moves into the sidebars ([c36992d](https://github.com/xseman/pando/commit/c36992d90443c61136e8387328d3070796f1a067))
* **ui:** the sidebar strips read in title case ([7aca2a9](https://github.com/xseman/pando/commit/7aca2a9cc7f4ea33b4a017090d561e436d0c9eb0))
* **ui:** tree chevrons are VS Code's codicon twisties with Nerd Font icons ([d1930b9](https://github.com/xseman/pando/commit/d1930b9ee7d1e64ae2d35f1d64cbc0c567e5bd44))


### Bug Fixes

* **daemon:** asleep sessions keep their name ([f973339](https://github.com/xseman/pando/commit/f973339b9f708ac7aab00fe1aec7aab2a8ff3a09))
* **daemon:** only an agent sleeps on its own ([482cb93](https://github.com/xseman/pando/commit/482cb9396799de44ab29bb589b94b25f1224690a))
* **daemon:** Updated moves on real work, not on a redraw's flip ([4eb1a40](https://github.com/xseman/pando/commit/4eb1a402539a6940a52858b9b09160263039e257))
* **lsp:** inserts at one position keep the server's order ([4dc3d21](https://github.com/xseman/pando/commit/4dc3d21b845cfb5d63bbde99193d9582a4c91cd9))
* **ui:** a long session name no longer drops its tab off the strip ([690df0d](https://github.com/xseman/pando/commit/690df0dceeee15686f7e8a7b9ad00c0a7b81f5a0))
* **ui:** a selected folded header with whitespace marks keeps its ⋯ ([c11e09d](https://github.com/xseman/pando/commit/c11e09d78c3a5768a4a32da5a3aa0e9a4a29aae0))
* **ui:** a session dropped on a session joins its strip ([8278c1e](https://github.com/xseman/pando/commit/8278c1e60a6973c6dc3d1b78cdef169c8a94aaee))
* **ui:** a session in a group without the keyboard keeps drawing ([783f524](https://github.com/xseman/pando/commit/783f5247ad8b5a0cb730eb0a5e6262de7aedfe91))
* **ui:** a terminal's screen no longer fetches the other terminals ([dfa6354](https://github.com/xseman/pando/commit/dfa6354d3fb7d27642c3b50b27af76b95455d237))
* **ui:** an esc with a mouse report behind it stays an esc under tmux ([df69687](https://github.com/xseman/pando/commit/df69687a457e831a3310290cf07cac7274875f55))
* **ui:** dimmed text is a color, not SGR faint ([7f87f8f](https://github.com/xseman/pando/commit/7f87f8f19a936395649b4360c43ea8c083d588ab))
* **ui:** key hints say alt+, not M- ([c3467c1](https://github.com/xseman/pando/commit/c3467c1f75454080ecdd304558973c15ed0083f2))
* **ui:** output no longer resizes a session between two TUIs ([867981d](https://github.com/xseman/pando/commit/867981dd08435a53a8a00792c6ec5ce68e20e9fc))


### Performance Improvements

* **ui:** a file's indent guides are counted once per text ([702e85a](https://github.com/xseman/pando/commit/702e85ae3498c1e25a5d82a58bf5e8899e1a7a99))
* **ui:** a frame takes what the last frame drew ([0abc389](https://github.com/xseman/pando/commit/0abc389ecf38997de8f243c629bd01684864e755))
* **ui:** a gutter's line number styles are built once per color pair ([86efe6b](https://github.com/xseman/pando/commit/86efe6b57adde46a94856082045ef1a15c66414e))
* **ui:** a modal measures its items once, not on every frame ([0f10cd0](https://github.com/xseman/pando/commit/0f10cd0eabbb88d569d234250f9d915e4e90c7d6))
* **ui:** a screen event fetches only the terminals of its session ([864971f](https://github.com/xseman/pando/commit/864971f26d1860b5dc56c77fc3d7e2c09f4c3969))
* **ui:** keys typed into a modal draw it over the frame drawn once ([f22b788](https://github.com/xseman/pando/commit/f22b7889a2411cac29f26e440bb6958fc31e64ad))


### Documentation

* **demo:** hero shows two sessions at work ([846528f](https://github.com/xseman/pando/commit/846528f5d63183f243ecc766ab955fbed4d29ecf))
* **demo:** re-record every GIF with panel borders ([6c0e20f](https://github.com/xseman/pando/commit/6c0e20f762ca722ca140ec37b2c4d04e65aa1457))
* **demo:** re-record the GIFs after a frame by frame review ([6f6e709](https://github.com/xseman/pando/commit/6f6e70984b295df613016d3f0be625827d3b203c))
* **demo:** record the tapes with panel borders ([8ffbc36](https://github.com/xseman/pando/commit/8ffbc36faeb3f4c341a578ba04186892fd537729))
* **keys:** alt+n's row names the resumed session too ([2cc3db8](https://github.com/xseman/pando/commit/2cc3db817331cd799047f5e805092f500c32762a))


### Build System

* **deps:** bubbletea v2.1.1, its renderer's flush about a quarter faster ([99a4039](https://github.com/xseman/pando/commit/99a40398fc030464d235dba085cdec946e609d02))


### Maintenance

* **ui:** pickers of sessions build their rows in sessionItems ([4d314c2](https://github.com/xseman/pando/commit/4d314c20a83ee961333778df0f6093501d0504ea))


### Testing

* **daemon:** the sleep goroutine reads no session the loop rewrites ([6e9be0a](https://github.com/xseman/pando/commit/6e9be0a9fb8ab396b4e9111cf45302e8f660ec26))
* **daemon:** torn sessions file without a race ([e56f507](https://github.com/xseman/pando/commit/e56f507d8bde08919f9d02127b4aa813e79bc9dd))

## [0.8.0](https://github.com/xseman/pando/compare/v0.7.0...v0.8.0) (2026-10-29)


### Features

* **daemon:** a first config picks nerd icons when a Nerd Font is installed ([1f9c17e](https://github.com/xseman/pando/commit/1f9c17e8070e41ee3096bd52ed2243c7b87c13ca))
* **font:** install Symbols Nerd Font Mono from the nerd-fonts release ([6618195](https://github.com/xseman/pando/commit/66181959b321f0d3ee5dcaa47cd8f9bc7a542b8f))
* **ui:** a file or a session dragged out carries its tab under the pointer ([505093e](https://github.com/xseman/pando/commit/505093ec4ff474ce1c2a3ac7bbb8a7e027614b70))
* **ui:** a folded Spaces project counts its sessions ([efa1b62](https://github.com/xseman/pando/commit/efa1b623e97ead676f32e062bde3581c719f50e0))
* **ui:** a Spaces project folds on its chevron only, its name switches to it ([6e911e5](https://github.com/xseman/pando/commit/6e911e5613e968b33d72b1c6f281db6e6b8d4b32))
* **ui:** a title starting with the session name's issue key replaces the name ([f0f3b28](https://github.com/xseman/pando/commit/f0f3b288ca57b157253e9c228ce19dd28993e1de))
* **ui:** drag an Explorer file or a Spaces session into editor groups ([e7ef84b](https://github.com/xseman/pando/commit/e7ef84b900ec777f0a495e7b5b40e94b48510ded))
* **ui:** session tabs move among the files' tabs ([2e88b39](https://github.com/xseman/pando/commit/2e88b39f12c4e498e7c47a4c4fa822edbe99c581))
* **ui:** the harness picker lists the newest sessions' harnesses first ([819b398](https://github.com/xseman/pando/commit/819b398a63453bd0443a38625a244547591620b6))
* **ui:** the status bar offers a Nerd Font when none is installed ([c5f04f2](https://github.com/xseman/pando/commit/c5f04f24bc4b0f7f2f50166980f7c3628add8973))


### Bug Fixes

* **daemon:** an agent's screen says blocked where /proc is missing ([a5e5227](https://github.com/xseman/pando/commit/a5e52279614de544203f5f4f01fca923f8b52978))
* **ui:** a lit group divider no longer panics on an empty frame list ([eb93b32](https://github.com/xseman/pando/commit/eb93b326c56a8c2e619a4f973d642deef60084ce))


### Performance Improvements

* **ui:** a drag draws only its overlays over a frame drawn once ([4be9186](https://github.com/xseman/pando/commit/4be9186ace96a36b4ccc8b9b14e00f3c734cc4bc))
* **ui:** mouse motion draws a frame per 1/60 s, not per motion ([ffa44e9](https://github.com/xseman/pando/commit/ffa44e955b4b110510e89b7650295c55eeb2a4be))
* **ui:** wcSafe line by line, a line the last frame had reused ([fbdd830](https://github.com/xseman/pando/commit/fbdd8309e3099efa82d29b6dfc465d2234f0b8e8))


### Testing

* **font:** the offer test installs into the platform's own font directory ([052e646](https://github.com/xseman/pando/commit/052e6466ecda0645247e9c7e3a880e7591f7e355))

## [0.7.0](https://github.com/xseman/pando/compare/v0.6.0...v0.7.0) (2026-10-28)


### Features

* **config:** Spaces docks on the right by default ([b357326](https://github.com/xseman/pando/commit/b357326b86e966a3bfcba70ee452fe950ada3c54))
* **daemon:** list codex, agy and grok subagents too ([d91649c](https://github.com/xseman/pando/commit/d91649c2d2681a22466c3288f9b6bc5e53570b2f))
* list a claude session's running subagents in Spaces ([9260c9d](https://github.com/xseman/pando/commit/9260c9d626b818b81b313653dc3f5b0cc7b4c0f5))
* **ui:** a double click on a divider evens the two groups beside it ([36744e7](https://github.com/xseman/pando/commit/36744e7040feef1edce4f6e43279573003da7d5d))
* **ui:** a group added or closed shares the room evenly ([87acc79](https://github.com/xseman/pando/commit/87acc7926f8b89df859f512aa5a589382d1af4f7))
* **ui:** a linked worktree's ✕ in Spaces on hover deletes it ([b227a87](https://github.com/xseman/pando/commit/b227a870ec216de50698731ca0ec7e6b5ccf323b))
* **ui:** a rule over No Sessions folded too ([5bc05fc](https://github.com/xseman/pando/commit/5bc05fc2a4a247ec262ceeb515ac3c8b18769a44))
* **ui:** a search button in Spaces' header ([6b5c8fd](https://github.com/xseman/pando/commit/6b5c8fd0ac1e4e1bd03ba5349bd46ac48c1e9bb9))
* **ui:** a session's ✕ in Spaces on hover kills it ([c1fc850](https://github.com/xseman/pando/commit/c1fc850d98283a7d2874eda0bc5b9906d1e04a0e))
* **ui:** carry a dragged tab under the pointer, as VS Code does ([2c263d7](https://github.com/xseman/pando/commit/2c263d77e2078ed3d7b74fc02034898e3901f51a))
* **ui:** collapse or fold the Source Control drawers at once ([2ce0cb8](https://github.com/xseman/pando/commit/2ce0cb8ddc74adff41913eac66f2644a48da51a1))
* **ui:** compare HEAD with a picked branch, tag or typed revision ([e1d4d26](https://github.com/xseman/pando/commit/e1d4d26886abe50354bb0793168e39b924c8c863))
* **ui:** ctrl+p and ctrl+shift+p reach their palettes from anywhere ([1c0d1b5](https://github.com/xseman/pando/commit/1c0d1b5a8f8577f16102808fc7f2ea60435bd7ff))
* **ui:** ctrl+w or ctrl+d closes a split's group showing nothing ([2ae6a0a](https://github.com/xseman/pando/commit/2ae6a0ac199120de567c6d37e2ee56e5fd9be3ba))
* **ui:** discard a folder's changes in Source Control ([da68dd5](https://github.com/xseman/pando/commit/da68dd5297aad80c36ea03966f724b4f3175946d))
* **ui:** editor groups in place of the docked session column ([bf85723](https://github.com/xseman/pando/commit/bf8572300c34fcfe1181f42a5ed89b3b7a517456))
* **ui:** frame the focused group, pulse one whose session needs you ([c82a617](https://github.com/xseman/pando/commit/c82a61750825300d7b0d583d6143527685ba79e5))
* **ui:** light a diff's hidden-lines row under the mouse ([4603941](https://github.com/xseman/pando/commit/460394142de08e67a6333fcb44fce1252f564dca))
* **ui:** n on the welcome screen starts a session ([74146ef](https://github.com/xseman/pando/commit/74146ef9e1478ade770bfe91e5eb83d5c6583646))
* **ui:** No Sessions lists its projects a dimmed row each ([4186e1b](https://github.com/xseman/pando/commit/4186e1b8745fb56c73c5b9862ed7a5849343b40d))
* **ui:** session tabs in their Spaces glyph color ([4fd03de](https://github.com/xseman/pando/commit/4fd03de7f362364d539f963874627ec33b636736))
* **ui:** show a tab's close button while the mouse is on it ([ec7dba8](https://github.com/xseman/pando/commit/ec7dba8bdef267fb28b604fdfe5bb6c21aaf6543))
* **ui:** Spaces holds a lone checkout on its heading, groups by state ([db9cdc2](https://github.com/xseman/pando/commit/db9cdc23e2b88784187e8bd5fed49f3ae5500403))
* **ui:** Split Sessions in Spaces opens a worktree's sessions side by side ([59fe12e](https://github.com/xseman/pando/commit/59fe12ee7d674b530cfd70bc061ffec47e475a6b))
* **ui:** tint session tabs apart from file tabs ([d63e08b](https://github.com/xseman/pando/commit/d63e08baacb62717f2f687647cc2a8c54ae91e10))
* **ui:** toggle comments in every language chroma colours ([501a7e5](https://github.com/xseman/pando/commit/501a7e5aee9ab60b4bcdf1b0df6b7822ace9f4c8))
* **ui:** w, a and 3 on the welcome screen do what its text says ([351d9f6](https://github.com/xseman/pando/commit/351d9f696fc63ca27a92f51b41b1318f6ca635fa))


### Bug Fixes

* **daemon:** a redraw under claude's idle prompt is not running ([21c313e](https://github.com/xseman/pando/commit/21c313ec2288c95f42adfb8d8840c38b5e816600))
* **daemon:** a restart keeps a session's Updated ([eab0e3e](https://github.com/xseman/pando/commit/eab0e3eb27bab0fccf60c5356b5f55446b13d505))
* **proto:** start the daemon in a systemd scope of its own ([eecc07d](https://github.com/xseman/pando/commit/eecc07d1ede88d638e085dcb010160272e1fa743))
* **ui:** a click on the session on screen keeps its tab ([0474ebc](https://github.com/xseman/pando/commit/0474ebc38c6560a0755283fb5f529e12d6468acd))
* **ui:** a darker green for ok in the light theme ([0d08c4e](https://github.com/xseman/pando/commit/0d08c4e963318873c3746f44beabd81e9eb1c56a))
* **ui:** a session shown without a tab keeps one when another opens ([1ebd5d2](https://github.com/xseman/pando/commit/1ebd5d248ab53a9cdb8db846428540559e1bc030))
* **ui:** ctrl+/ comments a whole selection and takes it back ([18626c7](https://github.com/xseman/pando/commit/18626c7e7c5e21d1b3b5f1a80855c5bb316b50ec))
* **ui:** keep the folder picker's path readable without focus ([49b3c12](https://github.com/xseman/pando/commit/49b3c12747eb8e19bf2c8f0986f4fde4cbbfbdd5))
* **ui:** offer no split for a group's only tab dragged over its own group ([b5a0fe4](https://github.com/xseman/pando/commit/b5a0fe472da7aac7ac947159718737a23d9f9b57))
* **ui:** put the status bar's border right on the bar ([da66fd8](https://github.com/xseman/pando/commit/da66fd8a10c0e86129331bded80916591dc3f070))


### Documentation

* bring the docs in line with the last week's changes ([2fecdfe](https://github.com/xseman/pando/commit/2fecdfed7681407b424ea64adbd3b419f2971119))
* **demo:** a hero still with Spaces on the right, tui.gif in the tour ([2367e5f](https://github.com/xseman/pando/commit/2367e5fb2d344e70291e130cace3d96ce8e43237))
* **demo:** hero with a file and a session side by side, groups.gif ([66c44ce](https://github.com/xseman/pando/commit/66c44ce23ac54783c5692a1b2777e141bfe3420b))
* **demo:** hero with the activity bar ([8be7869](https://github.com/xseman/pando/commit/8be786939ba018b1afd7f2583e7ed30db8980e68))
* **demo:** re-record every GIF and the hero ([76013bc](https://github.com/xseman/pando/commit/76013bca94a3f7c9065654c9c2c4d98ad3884502))
* editor groups replace the session column ([a4af052](https://github.com/xseman/pando/commit/a4af052006df2e92a9c7ec98364efdab3daa2aee))
* **skill:** name sessions after the house style, branch or task ([27dcee9](https://github.com/xseman/pando/commit/27dcee9105efd11ec168b2aad7e840e0c4178391))


### Testing

* **e2e:** pin the right sidebar empty, Spaces starts there now ([fe65877](https://github.com/xseman/pando/commit/fe658773e3af7e644a85895514a76bc07ac23e70))
* **windows:** give PowerShell 45s to start on a slow runner ([bf13824](https://github.com/xseman/pando/commit/bf13824780eb106ae1371f3413216bf6be46db57))

## [0.6.0](https://github.com/xseman/pando/compare/v0.5.0...v0.6.0) (2026-10-20)


### Features

* **config:** put the activity bar down the sidebar edge by default ([8e4ea57](https://github.com/xseman/pando/commit/8e4ea572b75191632f19ae9322eb44a46699ec4f))
* **daemon:** keep named databases in config.toml's [databases] ([6db19f0](https://github.com/xseman/pando/commit/6db19f0e44859bbaebaf7d9633c1b9d4781524c9))
* **daemon:** keep pinned Spaces sessions in state.json ([38f2fdb](https://github.com/xseman/pando/commit/38f2fdb0a4463c73c950d32b5e4852cac8c90f57))
* **daemon:** put sessions to sleep and wake them ([924d119](https://github.com/xseman/pando/commit/924d1196c942b5562d2798be071b4d30b08188be))
* **daemon:** tell a screen's width and the directory in front ([bbe71e6](https://github.com/xseman/pando/commit/bbe71e63e7671131f82cf99a8414b07180e58aca))
* **db:** read and edit SQLite, Postgres and MariaDB through their clients ([f6b0fd8](https://github.com/xseman/pando/commit/f6b0fd84d3a28d66e58a9e5f939844d947f2f8ff))
* **git:** blame one line of the editor's text ([f6cabb6](https://github.com/xseman/pando/commit/f6cabb62d5395db7fbf39e56ea41fcd599c6592c))
* **git:** list files by name pattern, ignored ones too ([ecb6f44](https://github.com/xseman/pando/commit/ecb6f4438ea20793eb36d32fe5b600445abcde0e))
* keep a scrolled-back session page on its text ([db3ff64](https://github.com/xseman/pando/commit/db3ff6499fe9e3fc45ebb886f78aa7e0c7ba3fdc))
* **lsp:** ask for hover, signature help and formatting ([5d41bd0](https://github.com/xseman/pando/commit/5d41bd0d4a1a177b8e2f59a8238a26c77aa2c869))
* open FILE:LINE:COL from the command line ([6312f31](https://github.com/xseman/pando/commit/6312f31efa9761c09c1f115d20bc5d89bd79eab4))
* refuse a nested TUI inside a pando session ([e52b3c1](https://github.com/xseman/pando/commit/e52b3c1e37bb1791848db72bd8cd4686ba836425))
* run separate pando instances with --instance ([292eeb6](https://github.com/xseman/pando/commit/292eeb66f182f3b2c2f3a8747cd68c0138189b0b))
* show language servers' diagnostics ([7a2cdca](https://github.com/xseman/pando/commit/7a2cdca64b304c2b5ccb8f9e82ddcfd956e290f3))
* **ui:** a bottom panel with Problems and Output; references inline ([855d48b](https://github.com/xseman/pando/commit/855d48b72220493cc0adb0df2b069bd629b3eba2))
* **ui:** a prompt header and streaming results in Search ([89e2c62](https://github.com/xseman/pando/commit/89e2c62306cf93f7b38514fe789ccc3c9288a0ea))
* **ui:** add a Databases view and a table grid ([375047f](https://github.com/xseman/pando/commit/375047fe03c1c6b1ca48c5a2bbe7f6c6eede4d18))
* **ui:** add a tooltips setting to turn the button tips off ([22e71c0](https://github.com/xseman/pando/commit/22e71c0c9ad5ecc7d3f94b9b7eac905e5cf5cdec))
* **ui:** add projects from a folder picker ([efb7503](https://github.com/xseman/pando/commit/efb7503c93242136b5cbc773e2895f1b4343a8ac))
* **ui:** blame the cursor's line in the status bar ([dbc1339](https://github.com/xseman/pando/commit/dbc133908cef0e96b421e9a98d8c3738a4a31ee0))
* **ui:** describe binary files instead of their bytes ([faed33c](https://github.com/xseman/pando/commit/faed33c0cdad695f5b44badfb24c43a396c8329e))
* **ui:** dock a session beside an empty editor ([62ca895](https://github.com/xseman/pando/commit/62ca895c89cc016b074cb5999cc45ac8e5f4d881))
* **ui:** draw a border over the bottom Terminal panel ([cd1f220](https://github.com/xseman/pando/commit/cd1f2206095b3bda281d193f750e7c20dde60abc))
* **ui:** draw a rule over each open Spaces project ([c58f316](https://github.com/xseman/pando/commit/c58f31670b18d7d5e90b59dea00f67e1414e614d))
* **ui:** draw indent guides in every tree ([f748829](https://github.com/xseman/pando/commit/f748829ae78b697daa3a0a694ed97632c142e704))
* **ui:** draw indent guides in the editor ([9bb7152](https://github.com/xseman/pando/commit/9bb715216d80f5ca70a4326ea5a58b79d30cbf7f))
* **ui:** draw indent guides in the Explorer tree ([351f079](https://github.com/xseman/pando/commit/351f0792dcbe622a3ca48f571b8f27183ca26d00))
* **ui:** draw the status bar's rule through the middle of a row ([0432679](https://github.com/xseman/pando/commit/0432679de8655a4cfdd091c849c87d199c93dc61))
* **ui:** drop New Tab in Session and the Spaces tab count ([f0491e4](https://github.com/xseman/pando/commit/f0491e4d55fb69084bb106e57da24cf22485f3e4))
* **ui:** expand a diff's hidden lines in place ([32e9cb4](https://github.com/xseman/pando/commit/32e9cb40490a262943ab5974fbe61fe024049523))
* **ui:** fold code, light the cursor line, finer scrollbars ([c5628d3](https://github.com/xseman/pando/commit/c5628d3e2dc2704e077b21572a3159bc0b434de2))
* **ui:** format with the language server when no tool is set ([d2bd5fd](https://github.com/xseman/pando/commit/d2bd5fdb5e66db858cf4a79813ed1ae14f103b04))
* **ui:** frame Spaces' Pinned as a resizable pane, under its worktrees ([638dff9](https://github.com/xseman/pando/commit/638dff9f7f6aed2d9860990e47594afd29ba7fee))
* **ui:** group context menus, square corners at the pointer ([8e501d2](https://github.com/xseman/pando/commit/8e501d214a543bcd086fceaa18dc6d400a61ec8b))
* **ui:** indent as .editorconfig and the file's own lines say ([58d7592](https://github.com/xseman/pando/commit/58d75920d48afdabaec82ac1480cfd68fe285a84))
* **ui:** keep a worktree's maximized session, file or Terminal across restarts ([2ec4565](https://github.com/xseman/pando/commit/2ec4565cb5d168b9e1fd019f8c7a3f66e3b2590e))
* **ui:** keep each project's GitHub lists and fetch them again when old ([c6210cb](https://github.com/xseman/pando/commit/c6210cbfe5adcfe5e4a58626685ea0823922cb31))
* **ui:** keep each session's own maximized state ([210669d](https://github.com/xseman/pando/commit/210669d66f1990e95adf1051929802f58f5c39e2))
* **ui:** lay a GitHub pull request or issue out as its conversation ([c4978d6](https://github.com/xseman/pando/commit/c4978d66c5c40ec09c23e0526852c61967328a64))
* **ui:** lay the activity bar and status bar out as VS Code's ([3d3e357](https://github.com/xseman/pando/commit/3d3e357a3b127d7de6ee070f0049d2625960e7be))
* **ui:** leave scrolling to a session app that takes the mouse ([440e0b5](https://github.com/xseman/pando/commit/440e0b58f9560cb0bc78e5e48d32fe8d0a2833de))
* **ui:** light status bar items and dialog buttons under the pointer ([d2ae7ca](https://github.com/xseman/pando/commit/d2ae7ca44ac9c5f5648e9408841e6ba47efb2c92))
* **ui:** light the Explorer's active indent guide ([3fdf529](https://github.com/xseman/pando/commit/3fdf529ff461a6ef2bdb3d419d55060f8be25ceb))
* **ui:** Light+ code colors on a light terminal ([f37fe1f](https://github.com/xseman/pando/commit/f37fe1f687a3e21c9be763d6ae8664ab02ebb5a8))
* **ui:** mark a diff's changes on its scrollbar ([9c319b1](https://github.com/xseman/pando/commit/9c319b1aafa33c485b482216b72cb1d02149d56b))
* **ui:** mark a file's changes on its scrollbar ([af14a09](https://github.com/xseman/pando/commit/af14a09443414f6111c70c94fa8e1b5c01db0c64))
* **ui:** mark changed lines in the editor's gutter, VS Code's quick diff ([d74a808](https://github.com/xseman/pando/commit/d74a808e1887238c128fa85d3f8995f95e6a95ef))
* **ui:** mark the session on screen in Spaces, untinted rows ([5de0977](https://github.com/xseman/pando/commit/5de09778883751c03883686ab7e078b00419abde))
* **ui:** mark what a picker's query matched, and say when nothing does ([9b03964](https://github.com/xseman/pando/commit/9b03964213c768703e95fcdb8403254b658d5caf))
* **ui:** name an icon button and its key when the pointer rests on it ([da5e520](https://github.com/xseman/pando/commit/da5e5208eda7c7e59dc67fcf854284d51551059b))
* **ui:** open a terminal's links with ctrl+click ([bb9171e](https://github.com/xseman/pando/commit/bb9171ead97c52467a9b17eb9a129001da7f1d55))
* **ui:** open the GitHub view with only pull requests unfolded ([37e3e87](https://github.com/xseman/pando/commit/37e3e8746f5d3a8c5a6cd6422b2bc51f6d8cd78a))
* **ui:** pick the indentation in one dialog ([a1a9d90](https://github.com/xseman/pando/commit/a1a9d906f867016c5818c86bca8447de72045313))
* **ui:** pin sessions on top of Spaces ([021d8c4](https://github.com/xseman/pando/commit/021d8c41b1034f843af14bf13b20c554f9faae7d))
* **ui:** resume a conversation from a worktree in Spaces ([888c6fe](https://github.com/xseman/pando/commit/888c6fe2b33f6dbb205d592fa1ef76ffc27df9f3))
* **ui:** resume from Spaces' header ([b0337a7](https://github.com/xseman/pando/commit/b0337a7fa3356513912e69e3484f2b090ac489b2))
* **ui:** reveal the editor's file in Explorer ([5e5299c](https://github.com/xseman/pando/commit/5e5299c90e7c3c069d8e07146036848ae7240c8a))
* **ui:** rule the inline References off the code around them ([ab56491](https://github.com/xseman/pando/commit/ab5649161ba26250ec15333e9246a79eb45791f7))
* **ui:** say in the merge dialog when a pull request's checks fail ([651060e](https://github.com/xseman/pando/commit/651060e0fbb1d0ad08ec687e2fbf71383db82b74))
* **ui:** sessions as tabs in the editor strip, dragged out to dock ([7c46580](https://github.com/xseman/pando/commit/7c46580b58fe7d30fc0f801a8fc78cbad3f501fb))
* **ui:** set Spaces' Pinned apart by one heavy border, no resize ([d9d641a](https://github.com/xseman/pando/commit/d9d641a1a37fe37b591292550d4824d9eb90f88e))
* **ui:** Settings as a tab, a Manage menu under the gear and F10 ([98c4d27](https://github.com/xseman/pando/commit/98c4d27dd97204a27324fb057fff8b40633bb867))
* **ui:** show a cut session name whole in a tip ([2ce07e6](https://github.com/xseman/pando/commit/2ce07e63cb4056f82102ce3bb0cc687afa92b9e3))
* **ui:** show a GitHub page's button tips by the pointer ([1fbdc4f](https://github.com/xseman/pando/commit/1fbdc4f0730e4c216116093871231a7c5f4b1ff5))
* **ui:** show an agent's state on a tab or terminal that runs it ([7231ea7](https://github.com/xseman/pando/commit/7231ea705563e9c3d6469948a84db33741b3ca39))
* **ui:** show the language server's hover and parameter hints ([43f2532](https://github.com/xseman/pando/commit/43f2532bcd558cd1ca1f9dc300e5e80a311bcd5a))
* **ui:** show the one last used when the session and the file share the editor ([ddace10](https://github.com/xseman/pando/commit/ddace10c3dc68e3e1b02b0bbd6bd659c53823cc1))
* **ui:** show the Spaces pin button on hover only ([70ced37](https://github.com/xseman/pando/commit/70ced37e8b19719fe9252b2936de4205dc628816))
* **ui:** sleep and wake sessions from Spaces ([66a17af](https://github.com/xseman/pando/commit/66a17af86b82f1b99c7742e6b30e58833aa7ff98))
* **ui:** soft panel frames and column dividers in panel_border ([10fe745](https://github.com/xseman/pando/commit/10fe745da6570c6b3d75b2e0cb03915fc28cb131))
* **ui:** step through a diff's changes and files ([e2af73d](https://github.com/xseman/pando/commit/e2af73dbb4cefa7fa1966ad010c9ed61426edec2))
* **ui:** take the light theme's colors from Simple Light ([9b05f50](https://github.com/xseman/pando/commit/9b05f50fcec395ee4e1eacc1cc3b566ac780366c))
* **ui:** turn a working session's ◐ like a spinner ([a4d270d](https://github.com/xseman/pando/commit/a4d270d0cc8965d984562b1d886655dc3c258862))


### Bug Fixes

* **daemon:** broadcast sessions when viewing clears attention ([652a352](https://github.com/xseman/pando/commit/652a35200e0c85c21ed0ff1550a187f1f650a910))
* **daemon:** forget what a removed workspace had open ([acdf94c](https://github.com/xseman/pando/commit/acdf94c4e76b7a9066e11ecd5c7841f1fe360ef7))
* **daemon:** keep a session running until it first prints ([d9fb70e](https://github.com/xseman/pando/commit/d9fb70eef71518f25f1d3c363eb6e0aaaa44ca69))
* **daemon:** keep a session running while its agent streams output ([0700250](https://github.com/xseman/pando/commit/0700250e7961b18b3a4b262cc8b0b7020bdd1bf6))
* **daemon:** order Spaces by input and status, not output ([b93888f](https://github.com/xseman/pando/commit/b93888fe7e91c60987ef79e317a5d9f639577e55))
* **daemon:** stop the jobs of a killed session's Terminal shells ([7e0c393](https://github.com/xseman/pando/commit/7e0c39305d454d8e07942d3c9d4a3fd83cceee2d))
* **ui:** click Source Control's and Search's buttons where they are drawn ([79cee96](https://github.com/xseman/pando/commit/79cee96b2dc6cdcc3ddb043d886f16288ac7a87b))
* **ui:** draw indent guides over render_whitespace's marks ([b280ef9](https://github.com/xseman/pando/commit/b280ef98446f99c8756bdacef733ffe5525468a3))
* **ui:** fit a row whose keycap emoji outmeasures its truncation ([49750ec](https://github.com/xseman/pando/commit/49750ecbdc42f65fb1dc30c58721159bd5429c8d))
* **ui:** fit hint rows, menus by key at the cursor, tidy boxes ([913ec73](https://github.com/xseman/pando/commit/913ec73e68f0cebbc379ac42516122e9cc5ba132))
* **ui:** flash one line of a multi-line error ([6706218](https://github.com/xseman/pando/commit/67062184f39e48fed3abd8dd5c56eaf03c4394c8))
* **ui:** hints under a box, edited tables pinned, split diffs placed ([81eb06a](https://github.com/xseman/pando/commit/81eb06a43ef0b4999c48c91a0ab1a9bfbf625e24))
* **ui:** indent Source Control's files past their folder as VS Code ([be4c234](https://github.com/xseman/pando/commit/be4c234995378a43c43d5e91ae0877dbdc27e552))
* **ui:** keep a box's rows the window's width over wide runes ([1920fb2](https://github.com/xseman/pando/commit/1920fb26449d86afd34c3e1b31c2064e07a9c298))
* **ui:** keep a morph as wide as its new text, and shorter ([2196cba](https://github.com/xseman/pando/commit/2196cba7639f4b264cd41b2adf56a8ae19c9969e))
* **ui:** keep a side-by-side selection on one half ([1a92321](https://github.com/xseman/pando/commit/1a9232193851790219ebd50ed87bc6672a288de8))
* **ui:** keep a status bar item's tip off the bar in a framed layout ([1b232bf](https://github.com/xseman/pando/commit/1b232bf02fdf8bf1b36f8a5a3a2d1060888e6aff))
* **ui:** keep a worktree's maximized session when switching back to it ([44cce28](https://github.com/xseman/pando/commit/44cce2830fedd13b0de82c873030e627a1edad26))
* **ui:** keep emoji the renderer measures narrower from shifting rows ([8e05c76](https://github.com/xseman/pando/commit/8e05c7659a2e1f8a3d5f7a01d6e79d2bada35575))
* **ui:** keep problems squiggled on a selected side-by-side row ([204f553](https://github.com/xseman/pando/commit/204f5539381ffba37585a78f962dc30b7eb75d6e))
* **ui:** keep sidebar chips, buttons and headers whole when narrow ([542eee2](https://github.com/xseman/pando/commit/542eee201f2a785a8f348e596ae6b14d00b980fa))
* **ui:** keep table columns whole and pending SQL readable ([d967f96](https://github.com/xseman/pando/commit/d967f9601206e7eb2d1b3714dbf3de268330f00d))
* **ui:** keep the columns while a divider is dragged on a narrow screen ([b5e0ba5](https://github.com/xseman/pando/commit/b5e0ba588d4e8400bbd94e60502f53a5c4448838))
* **ui:** keep the references peek's match and line in view ([af00562](https://github.com/xseman/pando/commit/af005624dfb65cfe95011132997e80af257077d7))
* **ui:** keep unsaved edits when an open file is opened at a line ([f48e092](https://github.com/xseman/pando/commit/f48e092e406a1bbb06f7b063c25e2b169be74e9b))
* **ui:** leave an editor clean when undo returns to the saved text ([a151377](https://github.com/xseman/pando/commit/a151377990361bac2677b1b359ce9c1fabbfc40a))
* **ui:** leave the Commit button alone while other git work runs ([b79e616](https://github.com/xseman/pando/commit/b79e616c07376dec937f1dbc32eaae58698fbead))
* **ui:** let a tip follow the pointer from button to button ([a3c7cbc](https://github.com/xseman/pando/commit/a3c7cbc0abec2742f46a36061994f6579178fc8f))
* **ui:** light the cursor's word in pando's own blue again ([7238586](https://github.com/xseman/pando/commit/72385861627419c0b98118f4278ec00d9e8714ae))
* **ui:** place, size and paint overlays as the demos expect ([5fd8242](https://github.com/xseman/pando/commit/5fd824220355df9768da42bd2364281f72dcb723))
* **ui:** put a tip away on a click's release, and name as the palette does ([bf83c54](https://github.com/xseman/pando/commit/bf83c540e86a25311fb39c55092994f4c9e595af))
* **ui:** put the cursor back on esc only in the file it came from ([98775a2](https://github.com/xseman/pando/commit/98775a2253395ba169b3833bd1e27fa4023df97b))
* **ui:** quiet loading tabs, clearer hints, colors and joints ([599b7dc](https://github.com/xseman/pando/commit/599b7dcac01f3beb43b4bbd389e3eca3636dfa08))
* **ui:** quiet suggestions in comments, tidy diffs and their dialogs ([9741845](https://github.com/xseman/pando/commit/9741845de6df1f81a12bc449f2963e56c7a37175))
* **ui:** raise the quick diff's buttons under the mouse ([c17a313](https://github.com/xseman/pando/commit/c17a313e1c0ea2742e7697902dd3c9935bd26de9))
* **ui:** reveal a change's file in Explorer, over Spaces too ([e3f8c35](https://github.com/xseman/pando/commit/e3f8c35711a5c7ef3a59808d6d2b6a89834bbecd))
* **ui:** say 1 minute ago, not 1 minutes ago ([0e1e927](https://github.com/xseman/pando/commit/0e1e9279d93f05bcb2317a417cd022b720703bac))
* **ui:** stage or revert the cursor's row in a side-by-side diff ([0dc7de4](https://github.com/xseman/pando/commit/0dc7de44ad16de8f044d4f7e27d08734a1a39972))
* **ui:** take M-p in Search's boxes for Preserve Case ([71b7a22](https://github.com/xseman/pando/commit/71b7a220ea87651155a3d7215b0281cbcb9dd3e7))
* **ui:** turn a plain shell's ◐ only while a command runs, on the clock ([2fdaf3e](https://github.com/xseman/pando/commit/2fdaf3e6ceef10c3af56d77d49d6e0c90325ef13))
* **ui:** V-LINE selection, diff follows staging, diff line numbers ([24aad0b](https://github.com/xseman/pando/commit/24aad0bb6fa4cb53e597c715c6933b4f3c6e72ca))
* **ui:** visual mode's own hints, the placeholder dissolves in its color ([24b65a3](https://github.com/xseman/pando/commit/24b65a30b30ac3bfd631a83dd45b64c562c8a833))


### Performance Improvements

* **daemon:** keep a session's scrollback as rendered rows ([f0799d0](https://github.com/xseman/pando/commit/f0799d0dfbb007c712b0b6992bd86dc98dbb9a02))
* **git:** read status in one walk, and back off on slow worktrees ([8e8166a](https://github.com/xseman/pando/commit/8e8166a1578bda4c58ff41460b7b95f5df0e74ab))
* **ui:** draw a frame with less work ([73e3318](https://github.com/xseman/pando/commit/73e33188fc64a829a47a1e1179d7279e1509f8f6))
* **ui:** draw only the visible window of a long line ([10e65ae](https://github.com/xseman/pando/commit/10e65ae94a53ee485b6824acb722240d05961643))
* **ui:** keep editing large Markdown files responsive ([638ad7d](https://github.com/xseman/pando/commit/638ad7dc06e4f2f89c195190e15ed1f67dda85aa))
* **ui:** look up a session's links in linear time ([5693a37](https://github.com/xseman/pando/commit/5693a37bd17e3ba7e6fe4a6768d9891a2dbb15bb))
* **ui:** paint text from the escapes kept per style ([960b599](https://github.com/xseman/pando/commit/960b5995954a191fa62941fa406516749f7eeab0))
* **ui:** reuse the frame after events that change nothing ([b0538e0](https://github.com/xseman/pando/commit/b0538e043b249c21c0fd62ca251d79c370fc8cd9))
* **ui:** type in large files without matching lexers again ([028eb74](https://github.com/xseman/pando/commit/028eb745153fc89dc5e20d549a416f312954b9b4))


### Documentation

* **config:** describe spaces_project_sort by input and status ([da5d282](https://github.com/xseman/pando/commit/da5d282c83c6804f65865d616a3191df46e1cef8))
* **demo:** cut tui's waits for blocked and done together ([6f61dbe](https://github.com/xseman/pando/commit/6f61dbe4b441bf114eaadd335ab648059e598361))
* **demo:** drop the CLI GIF and re-record the rest ([1730077](https://github.com/xseman/pando/commit/1730077b8434e951863daba3f2c0bb2f37145281))
* **demo:** drop the CLI recording ([82f097d](https://github.com/xseman/pando/commit/82f097d6a5d0b25eff3a7d1d80a52e7b08edbf04))
* **demo:** end tui on the session, the committed diff closed ([80d2c56](https://github.com/xseman/pando/commit/80d2c56e4dbc7e5aaec337b4985abe193904ae91))
* **demo:** give each step the time and room it needs ([1c5f500](https://github.com/xseman/pando/commit/1c5f50018f2178f063b60215dc718d33e8b05e5a))
* **demo:** give lsp's sidebar the cell Source Control's name needs ([ea7ad7e](https://github.com/xseman/pando/commit/ea7ad7ea36e36e8cb441fbba9468f95bbb4e3c70))
* **demo:** hold Pending SQL, end github before its message clears ([3b72d35](https://github.com/xseman/pando/commit/3b72d356d42344fe7bf2a6bd3df67443267dd169))
* **demo:** leave vim's insert mode with one esc ([b9357f6](https://github.com/xseman/pando/commit/b9357f60fcfa16157de8bb2d1b3fde42dc57fc5a))
* **demo:** make every tape record what it says ([ff7a68f](https://github.com/xseman/pando/commit/ff7a68f4bb05f37bba8b7f47598b5e052513f776))
* **demo:** re-record every tape ([7c435a3](https://github.com/xseman/pando/commit/7c435a3a989a5a960b2643eeeb672a08b4472e53))
* **demo:** re-record the GIFs with the panel, inline references and session tabs ([85127eb](https://github.com/xseman/pando/commit/85127ebecdafdea25d8ad58db6aa2749853fccf1))
* **demo:** record the Databases view ([0b157ae](https://github.com/xseman/pando/commit/0b157ae295aa5ad394ab92c9313c04caec85936c))
* **demo:** record with a steady cursor ([e9a2179](https://github.com/xseman/pando/commit/e9a217992fee39b361beb0744e5852a35a0f0d38))
* **demo:** say what CursorBlink holds still ([c6c2f75](https://github.com/xseman/pando/commit/c6c2f7565b739b8fd6138fdeeca3ab32f3aac4d1))
* **demo:** settle lsp's start and tui's idle, trim diff's focus step ([9e2fb7d](https://github.com/xseman/pando/commit/9e2fb7de02aec5805e59bf92dee84abf97fc6d4c))
* **demo:** wait for two agents running, not all three ([50402d8](https://github.com/xseman/pando/commit/50402d80e207fbd32ad9d96830c1b15c463156b1))
* describe the quick diff as a zone under its change ([20dd781](https://github.com/xseman/pando/commit/20dd78146dd535f7c30cf02ca04e67f89b7745b8))
* session sleep ([69b5fc1](https://github.com/xseman/pando/commit/69b5fc1d27aaa14825acd501c5920ab37bbce8f5))


### Maintenance

* run the database tests against Postgres and MariaDB ([baf34f3](https://github.com/xseman/pando/commit/baf34f3bc0e27a0b087bb2a2c60ecf455a060a70))
* **ui:** name the two-way choice and the tip's wake plainly ([9c9ce39](https://github.com/xseman/pando/commit/9c9ce39a8e233b0d5fb8b2424f108dfa998f2b74))
* **ui:** remove session tabs ([a9c1a83](https://github.com/xseman/pando/commit/a9c1a8303621eeaa2ba2ccf8e3e258ac6a447ef6))
* **ui:** share the Explorer's active indent guide ([3f8119a](https://github.com/xseman/pando/commit/3f8119a924930d26e18b516d4d0e7881c69439c5))


### Testing

* add benchmarks, allocation budgets and profile targets ([d49c44c](https://github.com/xseman/pando/commit/d49c44cea0851b9cfff6e0da538f0b09a26613c4))
* **ui:** drive the model with a chaos monkey ([d6e176d](https://github.com/xseman/pando/commit/d6e176dda58c7ec1d4dce3156a240378243dae82))
* **ui:** drop the view before renaming the table it reads ([e051062](https://github.com/xseman/pando/commit/e051062ff39c0f965a6b418fbf9d12e956573fb0))
* **ui:** keep the UI tests off the person's own daemon ([140c41d](https://github.com/xseman/pando/commit/140c41db38ddf0ed41fc1a5ff102a6f4ebcad034))
* **windows:** wait for the nested daemon's lock before removing its dirs ([09c9a97](https://github.com/xseman/pando/commit/09c9a97813cc3d04d75a0dcd6c131922d2192b43))

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
