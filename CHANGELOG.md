# Changelog

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
