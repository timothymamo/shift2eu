# Changelog

## [0.6.0](https://github.com/timothymamo/shift2eu/compare/v0.5.0...v0.6.0) (2026-11-14)


### Features

* **assessment:** workloads, blocker notes and the source cost (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M7) ([#217](https://github.com/timothymamo/shift2eu/issues/217)) ([0bcba5e](https://github.com/timothymamo/shift2eu/commit/0bcba5e0144de3911fa47aeb1e8e46f3fe418042))
* **catalog:** optional fixed monthly fee per offering for Kubernetes control planes ([#205](https://github.com/timothymamo/shift2eu/issues/205)) ([#209](https://github.com/timothymamo/shift2eu/issues/209)) ([3319b6f](https://github.com/timothymamo/shift2eu/commit/3319b6f99aec436f6f2d6f0eef3cdfa982e253a7))
* **catalog:** plans, prices and a default region in target snapshot format v2 (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M2) ([#200](https://github.com/timothymamo/shift2eu/issues/200)) ([ae0fd8d](https://github.com/timothymamo/shift2eu/commit/ae0fd8de24e469287fdaf3175df4390279b59348))
* **estimate:** honour UpCloud's 672-hour monthly billing cap ([#218](https://github.com/timothymamo/shift2eu/issues/218)) ([#221](https://github.com/timothymamo/shift2eu/issues/221)) ([783ff0c](https://github.com/timothymamo/shift2eu/commit/783ff0c1c7ffcefc32def232a1a8fc3ceb463dc8))
* **estimate:** plan matching, monthly estimates and regions (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M6) ([#214](https://github.com/timothymamo/shift2eu/issues/214)) ([d4ffb08](https://github.com/timothymamo/shift2eu/commit/d4ffb0892daea1d820120d2d96cdbf7ae1e58956))
* **inventory:** inventory format version 2 (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) milestone 1) ([#199](https://github.com/timothymamo/shift2eu/issues/199)) ([a5af276](https://github.com/timothymamo/shift2eu/commit/a5af276b9db203ea5ac4d5ee3b5ac74213636cee))
* **ovhcloud:** plans and monthly EUR prices in the catalog snapshot (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M3) ([#203](https://github.com/timothymamo/shift2eu/issues/203)) ([979281e](https://github.com/timothymamo/shift2eu/commit/979281e99124cb81e5c4069510188c423cea0b1c))
* **report:** estimates, workloads and region options in the reports, CLI and API (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M8) ([#222](https://github.com/timothymamo/shift2eu/issues/222)) ([c7dac77](https://github.com/timothymamo/shift2eu/commit/c7dac775b7b9041cd1c496586693232114c99245))
* **scaleway:** plans and monthly EUR prices in the catalog snapshot (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M4) ([#208](https://github.com/timothymamo/shift2eu/issues/208)) ([e57025f](https://github.com/timothymamo/shift2eu/commit/e57025f0b03502e1c92177f2ab3661b1d0129ecc))
* **upcloud:** plans and monthly EUR prices in the catalog snapshot (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M5) ([#210](https://github.com/timothymamo/shift2eu/issues/210)) ([5c585bf](https://github.com/timothymamo/shift2eu/commit/5c585bf4024c6ff457523925f0a23fa4bec6b5e2))


### Bug Fixes

* **lint:** count only a go.mod that is no directory as a nested module root ([#207](https://github.com/timothymamo/shift2eu/issues/207)) ([#215](https://github.com/timothymamo/shift2eu/issues/215)) ([ca30939](https://github.com/timothymamo/shift2eu/commit/ca30939b13067f7ec240c8751b5f42f9099f54e6))

## [0.5.0](https://github.com/timothymamo/shift2eu/compare/v0.4.0...v0.5.0) (2026-11-13)


### Features

* **taxonomy:** separate bare-metal servers from virtual machines ([#187](https://github.com/timothymamo/shift2eu/issues/187)) ([efd7496](https://github.com/timothymamo/shift2eu/commit/efd74964bd4ab0b6fae06bc8284dc1a5cf0136d9))


### Bug Fixes

* **lint:** make make check pass on main again ([#177](https://github.com/timothymamo/shift2eu/issues/177)) ([#180](https://github.com/timothymamo/shift2eu/issues/180)) ([a9573fd](https://github.com/timothymamo/shift2eu/commit/a9573fdf0a66230cb0e64e98d60585fd8e81504c))

## [0.4.0](https://github.com/timothymamo/shift2eu/compare/v0.3.0...v0.4.0) (2026-11-13)


### Features

* **openapi:** publish a bundled OpenAPI document with each release ([#139](https://github.com/timothymamo/shift2eu/issues/139)) ([#170](https://github.com/timothymamo/shift2eu/issues/170)) ([1616951](https://github.com/timothymamo/shift2eu/commit/1616951943429f0ad8b016db4edb243607e669f0))

## [0.3.0](https://github.com/timothymamo/shift2eu/compare/v0.2.0...v0.3.0) (2026-11-12)


### Features

* **dev:** set the deck's default_command for new agents in make vm-init ([#135](https://github.com/timothymamo/shift2eu/issues/135)) ([60b0e07](https://github.com/timothymamo/shift2eu/commit/60b0e0757566d743b8d0e23dc90e2eb874f12ff1)), closes [#134](https://github.com/timothymamo/shift2eu/issues/134)
* **dev:** set up a recreated VM with make vm-init and vm-create ([#131](https://github.com/timothymamo/shift2eu/issues/131)) ([0cff76f](https://github.com/timothymamo/shift2eu/commit/0cff76f47d048da7f8dafad5a4491bf756c7f9ab))
* **release:** ship shift2eu-server in its own release archives ([#162](https://github.com/timothymamo/shift2eu/issues/162)) ([1189e9d](https://github.com/timothymamo/shift2eu/commit/1189e9d7d27b1d34abc35447b5701f3038d8dc1a))
* **release:** ship shift2eu-server with GoReleaser and as a signed image (PRD [#103](https://github.com/timothymamo/shift2eu/issues/103) M4) ([#140](https://github.com/timothymamo/shift2eu/issues/140)) ([0c5e067](https://github.com/timothymamo/shift2eu/commit/0c5e067361c304a05441e3a5ca3f2157375a95cd))


### Bug Fixes

* **dev:** drop allow rules whose wildcard lets git and make options through ([#133](https://github.com/timothymamo/shift2eu/issues/133)) ([ae70409](https://github.com/timothymamo/shift2eu/commit/ae70409dd18134f412b01f32e22cc79b954c8b9a)), closes [#132](https://github.com/timothymamo/shift2eu/issues/132)
* **lint:** forbid process execution and environment reads in the composition roots ([#111](https://github.com/timothymamo/shift2eu/issues/111)) ([#150](https://github.com/timothymamo/shift2eu/issues/150)) ([064b9da](https://github.com/timothymamo/shift2eu/commit/064b9da598e2a621c7cfd1c14df038ef4e5336eb))
* **lint:** make adapters-are-built-in-cmd fire and guard every depguard rule against shadowing ([#119](https://github.com/timothymamo/shift2eu/issues/119)) ([#143](https://github.com/timothymamo/shift2eu/issues/143)) ([f13e4e3](https://github.com/timothymamo/shift2eu/commit/f13e4e32791a9f559225789d845e1b6bde572809))
* **lint:** make app-tests-use-fakes-not-adapters a strict allow list ([#144](https://github.com/timothymamo/shift2eu/issues/144)) ([#154](https://github.com/timothymamo/shift2eu/issues/154)) ([da346a8](https://github.com/timothymamo/shift2eu/commit/da346a8054f98305524bb868b60c32d1714fcae0))
