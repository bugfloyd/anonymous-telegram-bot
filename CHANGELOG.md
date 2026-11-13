# Changelog

## [2.0.0](https://github.com/incognitochat/incognitochat/compare/v1.0.0...v2.0.0) (2026-11-13)


### ⚠ BREAKING CHANGES

* v2, end-to-end encrypted messaging in a Mini App ([#58](https://github.com/incognitochat/incognitochat/issues/58))

### Features

* anonymous send tokens (blind RSA) ([#41](https://github.com/incognitochat/incognitochat/issues/41)) ([9d31445](https://github.com/incognitochat/incognitochat/commit/9d3144568ec59f44cc7a8022f6aa8d63b99b1ec0))
* **backend:** auth core for v2 ([#26](https://github.com/incognitochat/incognitochat/issues/26)) ([b30c199](https://github.com/incognitochat/incognitochat/commit/b30c1991539fcf9a39809503b12dd334ed1a0150))
* **backend:** invitations with levels, allowances and admin grants ([#34](https://github.com/incognitochat/incognitochat/issues/34)) ([1a46e1e](https://github.com/incognitochat/incognitochat/commit/1a46e1ea5c5056b49e5de818bf4e14a1fd472394))
* **backend:** links with signed bundles, create, rotate and revoke ([#32](https://github.com/incognitochat/incognitochat/issues/32)) ([744af7e](https://github.com/incognitochat/incognitochat/commit/744af7ed02809a910f93b573df2c2b4651e9df9a))
* **backend:** mailboxes and delivery ([#40](https://github.com/incognitochat/incognitochat/issues/40)) ([3089dee](https://github.com/incognitochat/incognitochat/commit/3089deeec0781848e82c53026c82d258ee53d026))
* **backend:** registration and device login ([#29](https://github.com/incognitochat/incognitochat/issues/29)) ([bdef371](https://github.com/incognitochat/incognitochat/commit/bdef3719b0dbefec9e434848dfc7462e58fb97e6))
* **bot:** open-app replies, tidy service messages, self-registered webhook ([#36](https://github.com/incognitochat/incognitochat/issues/36)) ([73feeab](https://github.com/incognitochat/incognitochat/commit/73feeab6091aba130df45f9ff7c3ea061ba41eb4))
* **ci:** build, attest and deploy the client from the hosting repository ([#37](https://github.com/incognitochat/incognitochat/issues/37)) ([2e4ad97](https://github.com/incognitochat/incognitochat/commit/2e4ad970f6e74672fce9c74ecffebf10c6b823df))
* **ci:** release with release-please and deploy releases to production ([#84](https://github.com/incognitochat/incognitochat/issues/84)) ([45b660c](https://github.com/incognitochat/incognitochat/commit/45b660ca4b09042a4345f9239d14311444ae12ff))
* delete an account ([#81](https://github.com/incognitochat/incognitochat/issues/81)) ([43a392c](https://github.com/incognitochat/incognitochat/commit/43a392c67b60be610ef6f09949912edd046bdca8))
* encrypted media uploads (server side) ([#62](https://github.com/incognitochat/incognitochat/issues/62)) ([fb75ad2](https://github.com/incognitochat/incognitochat/commit/fb75ad2ce9e18731c0403e6a35091ad9b4cede1b))
* **infra:** run only CI-signed Lambda code, and attest and check each deploy ([#83](https://github.com/incognitochat/incognitochat/issues/83)) ([d2292fd](https://github.com/incognitochat/incognitochat/commit/d2292fd8f17a97a7caae1c46648b3994a8e16b9f))
* **infra:** v2 infrastructure and dev deploys ([#27](https://github.com/incognitochat/incognitochat/issues/27)) ([16e7c77](https://github.com/incognitochat/incognitochat/commit/16e7c773f2b84b5fcc2c089d3dd28f718b12c146))
* one link per account, replaceable ([#49](https://github.com/incognitochat/incognitochat/issues/49)) ([a7dcb91](https://github.com/incognitochat/incognitochat/commit/a7dcb91cc37a13f21033c27a4714a71d740f65b8))
* push to open apps over WebSocket ([#53](https://github.com/incognitochat/incognitochat/issues/53)) ([03118b2](https://github.com/incognitochat/incognitochat/commit/03118b29dded473f2e4c97ef4a6123762b320e06))
* say when the account is on another device ([#52](https://github.com/incognitochat/incognitochat/issues/52)) ([9317da8](https://github.com/incognitochat/incognitochat/commit/9317da8c14d92be7e071a1d8f19454f3833db8d5))
* Telegram notices for new messages ([#47](https://github.com/incognitochat/incognitochat/issues/47)) ([b699bf7](https://github.com/incognitochat/incognitochat/commit/b699bf71007131b12a9a147a0d9315f447a11b68))
* v2 skeleton with reproducible builds ([#25](https://github.com/incognitochat/incognitochat/issues/25)) ([030b30d](https://github.com/incognitochat/incognitochat/commit/030b30d894f8db0c5f9ace5991f678ac1c187600))
* v2, end-to-end encrypted messaging in a Mini App ([#58](https://github.com/incognitochat/incognitochat/issues/58)) ([f1b6bb1](https://github.com/incognitochat/incognitochat/commit/f1b6bb120133cefbf88e6c4d9686a3e7ae44aab6))
* **web:** a message bar like Telegram's ([#77](https://github.com/incognitochat/incognitochat/issues/77)) ([9d0725b](https://github.com/incognitochat/incognitochat/commit/9d0725b7c44cc68c8e0a84b898d8f613cc5e64bb))
* **web:** attachments core (encryption, upload, download) ([#63](https://github.com/incognitochat/incognitochat/issues/63)) ([dd9fa71](https://github.com/incognitochat/incognitochat/commit/dd9fa71ddb0a4dfda51bdda9fd063f979e0b68c4))
* **web:** chats, conversations and writing to links ([#43](https://github.com/incognitochat/incognitochat/issues/43)) ([740f14a](https://github.com/incognitochat/incognitochat/commit/740f14a810ad148bb8ded8bc4cf4da1adf3d543e))
* **web:** check marks, and deleting conversations ([#48](https://github.com/incognitochat/incognitochat/issues/48)) ([c07adb5](https://github.com/incognitochat/incognitochat/commit/c07adb50d6a59a36e34f7f6015f0dd0fc345f129))
* **web:** client core for v2 ([#28](https://github.com/incognitochat/incognitochat/issues/28)) ([696a737](https://github.com/incognitochat/incognitochat/commit/696a7373ba4ab6894fe2e882a6cb6aeafe06b29e))
* **web:** create, share and revoke links, and check links others share ([#33](https://github.com/incognitochat/incognitochat/issues/33)) ([7d4d409](https://github.com/incognitochat/incognitochat/commit/7d4d4096d14b3f627c4a3e17ce5c35ceeccd59d9))
* **web:** join with an invitation, invite others, and admin settings ([#35](https://github.com/incognitochat/incognitochat/issues/35)) ([1447ca7](https://github.com/incognitochat/incognitochat/commit/1447ca7e4ef30ed251150035d026d708c753e500))
* **web:** live delivery over the push socket ([#54](https://github.com/incognitochat/incognitochat/issues/54)) ([59b89ac](https://github.com/incognitochat/incognitochat/commit/59b89ac0c55290292fd68a2909d1febc764cb716))
* **web:** message crypto, HPKE, envelopes and authenticated messages ([#39](https://github.com/incognitochat/incognitochat/issues/39)) ([226b5f4](https://github.com/incognitochat/incognitochat/commit/226b5f49e492a0621944093286535694aa83bd85))
* **web:** messaging engine, conversations and history on the device ([#42](https://github.com/incognitochat/incognitochat/issues/42)) ([0ba0c9a](https://github.com/incognitochat/incognitochat/commit/0ba0c9a4d1e047082829ce43abc455dd2b9da981))
* **web:** photos and files in conversations ([#65](https://github.com/incognitochat/incognitochat/issues/65)) ([0c17efe](https://github.com/incognitochat/incognitochat/commit/0c17efeb6c0c3f250d22b48fa57826add3cd1099))
* **web:** replies, and a message menu ([#61](https://github.com/incognitochat/incognitochat/issues/61)) ([eeb23ff](https://github.com/incognitochat/incognitochat/commit/eeb23ff81694b96bc40432955592d43941a0080f))
* **web:** round video messages, and hold to record ([#72](https://github.com/incognitochat/incognitochat/issues/72)) ([22f0210](https://github.com/incognitochat/incognitochat/commit/22f021024a6bfc66d1514cd26364ed5c7632fd2b))
* **web:** save photos and files through the device's browser ([#67](https://github.com/incognitochat/incognitochat/issues/67)) ([f9aeb1f](https://github.com/incognitochat/incognitochat/commit/f9aeb1f6ccca3c883a057318eb495e305b21815c))
* **web:** seen receipts and silent blocks ([#46](https://github.com/incognitochat/incognitochat/issues/46)) ([9fb5bc4](https://github.com/incognitochat/incognitochat/commit/9fb5bc426b6af8fdaa30e11c46ece7659d6f86c5))
* **web:** set up an account from Telegram ([#30](https://github.com/incognitochat/incognitochat/issues/30)) ([2a7e8ff](https://github.com/incognitochat/incognitochat/commit/2a7e8ffcb44b64d3747e5c7e9d60e10996df6e52))
* **web:** stickers ([#73](https://github.com/incognitochat/incognitochat/issues/73)) ([9555f8e](https://github.com/incognitochat/incognitochat/commit/9555f8e8bd844bec4b3912d5fa655e20e21fbe0a))
* **web:** Telegram-style chat scrolling, and seen only when in view ([#50](https://github.com/incognitochat/incognitochat/issues/50)) ([34831ce](https://github.com/incognitochat/incognitochat/commit/34831ce1383546f61fe37e9ff66206bd500b86a3))
* **web:** unblock, and blocking only when asked for ([#55](https://github.com/incognitochat/incognitochat/issues/55)) ([b5b156f](https://github.com/incognitochat/incognitochat/commit/b5b156fe105cc684858a93e44ec9c5172c691e2c))
* **web:** videos and GIFs in conversations ([#68](https://github.com/incognitochat/incognitochat/issues/68)) ([5aa5333](https://github.com/incognitochat/incognitochat/commit/5aa53334721bca37e96df643a3e85dbe265d840f))
* **web:** voice messages and music ([#71](https://github.com/incognitochat/incognitochat/issues/71)) ([d6738f5](https://github.com/incognitochat/incognitochat/commit/d6738f5e7412aa0261771eb9132fbc18af6d976b))
* **web:** weekly key rotation, and deleting old keys ([#51](https://github.com/incognitochat/incognitochat/issues/51)) ([9aaf0eb](https://github.com/incognitochat/incognitochat/commit/9aaf0eb11e30402826c5b865d65c6210cad35bc2))


### Bug Fixes

* admins and code owners can't redeem invitations ([#56](https://github.com/incognitochat/incognitochat/issues/56)) ([eb32bbb](https://github.com/incognitochat/incognitochat/commit/eb32bbb9d2a3b1f69b9361a8807ca13e9ec8eb4f))
* **infra:** restart functions when their parameters are rewritten ([#31](https://github.com/incognitochat/incognitochat/issues/31)) ([a59a497](https://github.com/incognitochat/incognitochat/commit/a59a497bfcded3b294e6de55fa8ba5850c5d9b5c))
* **infra:** size the api function from measurements, and log slow requests ([#45](https://github.com/incognitochat/incognitochat/issues/45)) ([7329581](https://github.com/incognitochat/incognitochat/commit/7329581604000a4046caea43d28f6ede69ccc1c8))
* send without waiting, and continue a conversation when its link reopens ([#44](https://github.com/incognitochat/incognitochat/issues/44)) ([794e448](https://github.com/incognitochat/incognitochat/commit/794e4486d3069ca83deadeb9987975edf9dabe9b))
* start clean after an account replacement, and replace dead mailboxes ([#57](https://github.com/incognitochat/incognitochat/issues/57)) ([fc25626](https://github.com/incognitochat/incognitochat/commit/fc25626f4dfaf35bce9f743210ef68573d45a0b8))
* **web:** keep a recording when a permission prompt takes the press ([#74](https://github.com/incognitochat/incognitochat/issues/74)) ([b183001](https://github.com/incognitochat/incognitochat/commit/b183001b3bc9ce4a3d5fe04517b07680fb72fc1c))
* **web:** keep each Telegram account's data apart on a shared Telegram app ([#38](https://github.com/incognitochat/incognitochat/issues/38)) ([659c563](https://github.com/incognitochat/incognitochat/commit/659c5634e5d7f22557ae2946d80124fef144ee43))
* **web:** keep the full-screen viewer out of its message ([#69](https://github.com/incognitochat/incognitochat/issues/69)) ([4d0b944](https://github.com/incognitochat/incognitochat/commit/4d0b944bea7f3df50e03fb1a5ee386b85baf4c61))
* **web:** keep the keyboard up after sending, and grow the message box ([#76](https://github.com/incognitochat/incognitochat/issues/76)) ([05e4d4d](https://github.com/incognitochat/incognitochat/commit/05e4d4da001913e8d28ea122cbd3aad89bb78664))
* **web:** make the viewer's buttons visible on light photos and videos ([#70](https://github.com/incognitochat/incognitochat/issues/70)) ([7ae1a21](https://github.com/incognitochat/incognitochat/commit/7ae1a21d27e0e2b41127ffeb672d88b0a6bf0f4b))
* **web:** show a photo or file the moment it's sent ([#66](https://github.com/incognitochat/incognitochat/issues/66)) ([ba9f064](https://github.com/incognitochat/incognitochat/commit/ba9f06431564242677c5d5f244a441a374be2576))
* **web:** show a video message in the chat as soon as it's sent ([#75](https://github.com/incognitochat/incognitochat/issues/75)) ([378c408](https://github.com/incognitochat/incognitochat/commit/378c40893844feee1e7b31b097cce9a318750634))
* **web:** sit the message bar low, as in Telegram ([#78](https://github.com/incognitochat/incognitochat/issues/78)) ([c09edbd](https://github.com/incognitochat/incognitochat/commit/c09edbd58ba5ad2f2ea286cbfe46c027a64abdd3))
