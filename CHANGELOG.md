# Changelog

## [0.8.0](https://github.com/timothymamo/shift2eu/compare/v0.7.0...v0.8.0) (2026-11-19)


### Features

* **aireview:** review pull requests with an AI reviewer that only comments (PRD [#161](https://github.com/timothymamo/shift2eu/issues/161) M4) ([#272](https://github.com/timothymamo/shift2eu/issues/272)) ([452c9f6](https://github.com/timothymamo/shift2eu/commit/452c9f68e4a023fcde1631f7c7250280205e01d0))


### Bug Fixes

* **devenv:** make the flaky devenv tests deterministic and fix the refresh lock takeover race ([#286](https://github.com/timothymamo/shift2eu/issues/286)) ([07f144a](https://github.com/timothymamo/shift2eu/commit/07f144a29c9c05449804a3f972711dab9ff7c36e))

## [0.7.0](https://github.com/timothymamo/shift2eu/compare/v0.6.0...v0.7.0) (2026-11-18)


### Features

* **devenv:** set the VM's CPUs, memory, disk and SSH port from make vm-up ([#228](https://github.com/timothymamo/shift2eu/issues/228)) ([#257](https://github.com/timothymamo/shift2eu/issues/257)) ([32c6760](https://github.com/timothymamo/shift2eu/commit/32c6760b9b82ad18597a0d37f7298d83b22eb026))
* **devenv:** vm-init sets default_dir, deck-restart reloads drop-ins, vm-env refuses missing credentials ([#234](https://github.com/timothymamo/shift2eu/issues/234)) ([#256](https://github.com/timothymamo/shift2eu/issues/256)) ([7095f01](https://github.com/timothymamo/shift2eu/commit/7095f01e4913f8fddcb400ad0ff135b43de54b3d))
* **estimate:** say when a plan's storage is not included ([#226](https://github.com/timothymamo/shift2eu/issues/226)) ([#266](https://github.com/timothymamo/shift2eu/issues/266)) ([8693f83](https://github.com/timothymamo/shift2eu/commit/8693f83c504c8d7bf8bad97c33b58486718ad781))


### Bug Fixes

* **devenv:** apply the agents App's bot identity to the VM's git when the token mint writes it ([#261](https://github.com/timothymamo/shift2eu/issues/261)) ([9d3700f](https://github.com/timothymamo/shift2eu/commit/9d3700f333dbae01ec5768602b6eeeba2a1765b5))
* **devenv:** make vm-create check the credentials before it creates the VM ([#258](https://github.com/timothymamo/shift2eu/issues/258)) ([#265](https://github.com/timothymamo/shift2eu/issues/265)) ([7eee61b](https://github.com/timothymamo/shift2eu/commit/7eee61b1d5a5dbfb36842365e528ad388d1b9276))
* **devenv:** make vm-up refuse a Broken or unknown Lima instance ([#237](https://github.com/timothymamo/shift2eu/issues/237)) ([#250](https://github.com/timothymamo/shift2eu/issues/250)) ([d0ecc21](https://github.com/timothymamo/shift2eu/commit/d0ecc213a3362276f76b9bea45e124637427e7d8))

## [0.6.0](https://github.com/timothymamo/shift2eu/compare/v0.5.0...v0.6.0) (2026-11-17)


### Features

* **assessment:** workloads, blocker notes and the source cost (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M7) ([#217](https://github.com/timothymamo/shift2eu/issues/217)) ([0bcba5e](https://github.com/timothymamo/shift2eu/commit/0bcba5e0144de3911fa47aeb1e8e46f3fe418042))
* **catalog:** optional fixed monthly fee per offering for Kubernetes control planes ([#205](https://github.com/timothymamo/shift2eu/issues/205)) ([#209](https://github.com/timothymamo/shift2eu/issues/209)) ([3319b6f](https://github.com/timothymamo/shift2eu/commit/3319b6f99aec436f6f2d6f0eef3cdfa982e253a7))
* **catalog:** plans, prices and a default region in target snapshot format v2 (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M2) ([#200](https://github.com/timothymamo/shift2eu/issues/200)) ([ae0fd8d](https://github.com/timothymamo/shift2eu/commit/ae0fd8de24e469287fdaf3175df4390279b59348))
* **catalog:** price or explain extra storage for OVHcloud and Scaleway ([#219](https://github.com/timothymamo/shift2eu/issues/219)) ([#225](https://github.com/timothymamo/shift2eu/issues/225)) ([b728e28](https://github.com/timothymamo/shift2eu/commit/b728e287ed3f502831bca5137b448899695d40ad))
* **devenv:** agents App token plumbing and bot identity (PRD [#161](https://github.com/timothymamo/shift2eu/issues/161) M1) ([#224](https://github.com/timothymamo/shift2eu/issues/224)) ([bbb7c94](https://github.com/timothymamo/shift2eu/commit/bbb7c947953c53f8cbce72c2aa8ed97e38204d38))
* **devenv:** gate checks, CODEOWNERS, skills and docs (PRD [#161](https://github.com/timothymamo/shift2eu/issues/161) M2) ([#239](https://github.com/timothymamo/shift2eu/issues/239)) ([998dc2f](https://github.com/timothymamo/shift2eu/commit/998dc2f549b1b653cf9591203202ab77da6f3ce8))
* **estimate:** honour UpCloud's 672-hour monthly billing cap ([#218](https://github.com/timothymamo/shift2eu/issues/218)) ([#221](https://github.com/timothymamo/shift2eu/issues/221)) ([783ff0c](https://github.com/timothymamo/shift2eu/commit/783ff0c1c7ffcefc32def232a1a8fc3ceb463dc8))
* **estimate:** plan matching, monthly estimates and regions (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M6) ([#214](https://github.com/timothymamo/shift2eu/issues/214)) ([d4ffb08](https://github.com/timothymamo/shift2eu/commit/d4ffb0892daea1d820120d2d96cdbf7ae1e58956))
* **examples:** examples, docs and e2e for sizes, workloads and estimates (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M9) ([#238](https://github.com/timothymamo/shift2eu/issues/238)) ([cc7bb27](https://github.com/timothymamo/shift2eu/commit/cc7bb2747b9833e42fc6e056427d78f838f835a5))
* **inventory:** inventory format version 2 (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) milestone 1) ([#199](https://github.com/timothymamo/shift2eu/issues/199)) ([a5af276](https://github.com/timothymamo/shift2eu/commit/a5af276b9db203ea5ac4d5ee3b5ac74213636cee))
* **ovhcloud:** plans and monthly EUR prices in the catalog snapshot (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M3) ([#203](https://github.com/timothymamo/shift2eu/issues/203)) ([979281e](https://github.com/timothymamo/shift2eu/commit/979281e99124cb81e5c4069510188c423cea0b1c))
* **report:** estimates, workloads and region options in the reports, CLI and API (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M8) ([#222](https://github.com/timothymamo/shift2eu/issues/222)) ([c7dac77](https://github.com/timothymamo/shift2eu/commit/c7dac775b7b9041cd1c496586693232114c99245))
* **scaleway:** plans and monthly EUR prices in the catalog snapshot (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M4) ([#208](https://github.com/timothymamo/shift2eu/issues/208)) ([e57025f](https://github.com/timothymamo/shift2eu/commit/e57025f0b03502e1c92177f2ab3661b1d0129ecc))
* **upcloud:** plans and monthly EUR prices in the catalog snapshot (PRD [#190](https://github.com/timothymamo/shift2eu/issues/190) M5) ([#210](https://github.com/timothymamo/shift2eu/issues/210)) ([5c585bf](https://github.com/timothymamo/shift2eu/commit/5c585bf4024c6ff457523925f0a23fa4bec6b5e2))


### Bug Fixes

* **devenv:** accept GitHub's new App installation token format ([#233](https://github.com/timothymamo/shift2eu/issues/233)) ([c641f78](https://github.com/timothymamo/shift2eu/commit/c641f7888bd642f2044396b8ec8d1312591eac25))
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
