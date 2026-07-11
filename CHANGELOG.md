# Changelog

## [0.3.0](https://github.com/DropMorePackets/berghain/compare/v0.2.0...v0.3.0) (2026-07-11)


### Features

* add challenge support IDs ([e472505](https://github.com/DropMorePackets/berghain/commit/e472505e6be978ea14dad64fdd5c70c769545718))
* add turnstile, hcaptcha, and recaptcha challenge types ([9839b07](https://github.com/DropMorePackets/berghain/commit/9839b07dc7f9983094958bc433e0753b68be61ff))
* allow skipping the captcha hostname check ([961647d](https://github.com/DropMorePackets/berghain/commit/961647dd832652d1ea1bfe01dd90972730c25085))
* **e2e:** exercise the turnstile challenge flow end to end ([0032fba](https://github.com/DropMorePackets/berghain/commit/0032fba2d04c373a067655ebdf7ddc567510cdc5))
* **haproxy:** add optional User-Agent policy ([#67](https://github.com/DropMorePackets/berghain/issues/67)) ([b1d88d9](https://github.com/DropMorePackets/berghain/commit/b1d88d90c0423e4519ae00de3b3c781b721bf554))
* **web:** add inline challenge customisation ([2f7a3de](https://github.com/DropMorePackets/berghain/commit/2f7a3de4b4b8d4346f1941dfb5ffcffbac5f1c6e))
* **web:** explain missing challenge capabilities ([0bab484](https://github.com/DropMorePackets/berghain/commit/0bab484d377f357c20c056e6a0e3ef0330b1c183))
* **web:** render captcha widgets for turnstile, hcaptcha, and recaptcha ([ab736d9](https://github.com/DropMorePackets/berghain/commit/ab736d9ae51a77eafa46d610470b3c3fd47924fd))
* **web:** skip countdown if zero ([34abd33](https://github.com/DropMorePackets/berghain/commit/34abd334c4747bcafe63dd202a11da0b969a78fb))
* **web:** toggle bootstrap elements ([d019e5a](https://github.com/DropMorePackets/berghain/commit/d019e5adcd4b1861d0848009f0406eee467fb8c4))


### Bug Fixes

* **spop:** normalize host authorities ([4672f8e](https://github.com/DropMorePackets/berghain/commit/4672f8e1b5011323e7ca2da57b425d5beef3c617))
* **web:** use named imports from @babel/core ([f272fe1](https://github.com/DropMorePackets/berghain/commit/f272fe13d81a76f8d91b24eac69c854496174318))

## [0.2.0](https://github.com/DropMorePackets/berghain/compare/v0.1.1...v0.2.0) (2025-06-18)


### Features

* **web:** add native-crypto build target ([35d28f1](https://github.com/DropMorePackets/berghain/commit/35d28f1f64131d9e719d3c6368daed15303b2c54))
* **web:** Use env files for different entrypoints ([ebabecb](https://github.com/DropMorePackets/berghain/commit/ebabecb6def2b6858b88a7b5178abed0defef3e8))

## [0.1.1](https://github.com/DropMorePackets/berghain/compare/v0.1.0...v0.1.1) (2025-06-06)


### Bug Fixes

* add wait-for-body to example configuration ([c006b4d](https://github.com/DropMorePackets/berghain/commit/c006b4d0f3d4590c30b8bdb9bee8f5dae3ef033b))
* handle cookie domain attribute for non-FQDNs ([a7a5166](https://github.com/DropMorePackets/berghain/commit/a7a51660dd42866dc708333b14bf19519a4c70fa))
* set challenge Content-Type and handle failure ([bdd7bc7](https://github.com/DropMorePackets/berghain/commit/bdd7bc72f77dbfc245992c0df8b6dbb7e3f6ce00))

## [0.1.0](https://github.com/DropMorePackets/berghain/compare/v0.0.1...v0.1.0) (2025-06-02)


### Features

* Implement cross domain validation ([#32](https://github.com/DropMorePackets/berghain/issues/32)) ([c825a31](https://github.com/DropMorePackets/berghain/commit/c825a31587744c0146af88c3d068f16f69853609))


### Bug Fixes

* switch to modern Sass JS API ([c83d5a1](https://github.com/DropMorePackets/berghain/commit/c83d5a1e103128de832423318a17dea697ee414d))
