# Changelog

## [0.12.0](https://github.com/timothymamo/shift2eu/compare/v0.11.0...v0.12.0) (2026-11-23)


### Features

* **ci:** add the audit's model-free publish core (PRD [#175](https://github.com/timothymamo/shift2eu/issues/175) milestone 2) ([#491](https://github.com/timothymamo/shift2eu/issues/491)) ([301c698](https://github.com/timothymamo/shift2eu/commit/301c69819fe553935d97139292997909137ab795))
* **ci:** share the Claude Code launcher with the audit, add its launcher, wrapper and sandbox preflight ([#277](https://github.com/timothymamo/shift2eu/issues/277)) ([#486](https://github.com/timothymamo/shift2eu/issues/486)) ([aae360c](https://github.com/timothymamo/shift2eu/commit/aae360c3231b176b4408c975c72c2baa40890961))
* **devenv:** start the Lima VM at boot with a LaunchDaemon of our own, and warn in vm-up about Lima's autostart ([#398](https://github.com/timothymamo/shift2eu/issues/398), [#315](https://github.com/timothymamo/shift2eu/issues/315)) ([#417](https://github.com/timothymamo/shift2eu/issues/417)) ([ca288cd](https://github.com/timothymamo/shift2eu/commit/ca288cd8759bec9d9e9f44e73ecfd703ea597270))
* **infra:** create the site's Scaleway hosting with OpenTofu in infra/site ([#422](https://github.com/timothymamo/shift2eu/issues/422)) ([#433](https://github.com/timothymamo/shift2eu/issues/433)) ([febe6cf](https://github.com/timothymamo/shift2eu/commit/febe6cffc8b2121bbbaa6ad4a91084475c7ed30a))
* **site:** a visual redesign of the site's pages and report ([#373](https://github.com/timothymamo/shift2eu/issues/373)) ([#379](https://github.com/timothymamo/shift2eu/issues/379)) ([20be9a2](https://github.com/timothymamo/shift2eu/commit/20be9a2a7d2e08672ed7f2ac8930568ffa3e7b67))


### Bug Fixes

* **aireview:** pin the Claude Code 2.1.330 integrity ([#428](https://github.com/timothymamo/shift2eu/issues/428)) ([#429](https://github.com/timothymamo/shift2eu/issues/429)) ([75b4149](https://github.com/timothymamo/shift2eu/commit/75b41492ded0ef1a27ac885afd9cbf62a2069adb))
* **browser:** bound the test-browser cleanup's docker rm -f ([#449](https://github.com/timothymamo/shift2eu/issues/449)) ([#481](https://github.com/timothymamo/shift2eu/issues/481)) ([f8963e1](https://github.com/timothymamo/shift2eu/commit/f8963e1896d3a6ce14d8ba7e1feae435344f67ee))
* **browser:** ignore a second signal in test-browser's traps before the cleanup runs ([#473](https://github.com/timothymamo/shift2eu/issues/473)) ([2a2526f](https://github.com/timothymamo/shift2eu/commit/2a2526f1f25c1a8e11056f65caa93afc26db9363))
* **browser:** pin the random DevTools port, a bundle per run, fail early on a dead container ([#423](https://github.com/timothymamo/shift2eu/issues/423)) ([#443](https://github.com/timothymamo/shift2eu/issues/443)) ([400e4dc](https://github.com/timothymamo/shift2eu/commit/400e4dcd2e84e98837bb0f699e8c7ba56961bcc8))
* **deps:** update module github.com/chromedp/cdproto to v0.160.8 ([#462](https://github.com/timothymamo/shift2eu/issues/462)) ([d4367cc](https://github.com/timothymamo/shift2eu/commit/d4367cc78c1560407143f3a6a5c0674c6595c416))
* **devenv:** count VM_ENV entries as typed, hide unset key shapes, one naming rule ([#304](https://github.com/timothymamo/shift2eu/issues/304) [#305](https://github.com/timothymamo/shift2eu/issues/305) [#306](https://github.com/timothymamo/shift2eu/issues/306)) ([#455](https://github.com/timothymamo/shift2eu/issues/455)) ([287892b](https://github.com/timothymamo/shift2eu/commit/287892be25568c2c2550d9c5d4b1f1b97dca525b))
* **devenv:** give the .envrc guard the host guard's file-identity resolver, shared ([#468](https://github.com/timothymamo/shift2eu/issues/468)) ([#472](https://github.com/timothymamo/shift2eu/issues/472)) ([d02df28](https://github.com/timothymamo/shift2eu/commit/d02df280cd2570d33920ef8b979a862c7ef50ab0))
* **devenv:** keep comments out of continued recipe commands for make 3.81 ([#437](https://github.com/timothymamo/shift2eu/issues/437)) ([#441](https://github.com/timothymamo/shift2eu/issues/441)) ([b0e1900](https://github.com/timothymamo/shift2eu/commit/b0e19000955ee9c1fd00f53da18fe8cf714facb5))
* **devenv:** keep the Lima VM's readiness probe off the gh wrapper ([#479](https://github.com/timothymamo/shift2eu/issues/479)) ([#480](https://github.com/timothymamo/shift2eu/issues/480)) ([b668bae](https://github.com/timothymamo/shift2eu/commit/b668baee7ab63722c5cd68bce81afef6dc1dc589))
* **devenv:** refuse the host targets when the credentials' .envrc is read from in or above the workspace ([#402](https://github.com/timothymamo/shift2eu/issues/402)) ([#430](https://github.com/timothymamo/shift2eu/issues/430)) ([289fdc1](https://github.com/timothymamo/shift2eu/commit/289fdc10c214a8e3a6f505ce68cd88661ee18d22))
* **devenv:** resolve the host-clone guard's workspace like the kernel, and keep the refresh's traps quiet under bash 3.2 ([#447](https://github.com/timothymamo/shift2eu/issues/447) [#448](https://github.com/timothymamo/shift2eu/issues/448)) ([#463](https://github.com/timothymamo/shift2eu/issues/463)) ([5e22031](https://github.com/timothymamo/shift2eu/commit/5e22031c20a367a8a3e025931dafff41bb674d76))
* **devenv:** take over a hand-started refresh, refuse foreign launchd jobs, and [#394](https://github.com/timothymamo/shift2eu/issues/394)'s lock cleanup follow-ups ([#407](https://github.com/timothymamo/shift2eu/issues/407)) ([56d8111](https://github.com/timothymamo/shift2eu/commit/56d8111a72a2e1557a92980b457045d51a2a47ee))
* **devenv:** VM_SHELL_ENV on every limactl call, re-set git hooks in vm-up, probe jq in the gate check ([#478](https://github.com/timothymamo/shift2eu/issues/478)) ([281f93a](https://github.com/timothymamo/shift2eu/commit/281f93a29f1f809821b0202caef4f6f29b4b3a99))


### Reverts

* **deps:** pin pi-coding-agent back to 1.5.2 ([#497](https://github.com/timothymamo/shift2eu/issues/497)) ([dba8f02](https://github.com/timothymamo/shift2eu/commit/dba8f02c02e2ff55d18238b57dacab75bfb43ff1))

## [0.11.0](https://github.com/timothymamo/shift2eu/compare/v0.10.1...v0.11.0) (2026-11-20)


### ⚠ BREAKING CHANGES

* shift2eu-server, the HTTP API, is retired and no longer released. Its images up to the last release stay on ghcr.io but get no updates. Run assessments with the CLI or in the browser on the site.

### Bug Fixes

* **aireview:** name CLI and answer errors by constant classes and summarise answered parts in a skip ([#374](https://github.com/timothymamo/shift2eu/issues/374), [#375](https://github.com/timothymamo/shift2eu/issues/375), [#388](https://github.com/timothymamo/shift2eu/issues/388)) ([#400](https://github.com/timothymamo/shift2eu/issues/400)) ([55be1fb](https://github.com/timothymamo/shift2eu/commit/55be1fb7e7e8f96a4e86a6a65e6ba6e7da96a975))
* **devenv:** remove a stale moved-aside refresh lock automatically, and run dev scripts by path ([#394](https://github.com/timothymamo/shift2eu/issues/394)) ([ef514be](https://github.com/timothymamo/shift2eu/commit/ef514beb6e6ac21bfaff598ee7e5c618304b87db))


### Miscellaneous Chores

* stop publishing shift2eu-server, the HTTP API ([#393](https://github.com/timothymamo/shift2eu/issues/393)) ([#401](https://github.com/timothymamo/shift2eu/issues/401)) ([87ea59b](https://github.com/timothymamo/shift2eu/commit/87ea59bb9733b9d1db9a86d62d6d4cbc01619e3f))

## [0.10.1](https://github.com/timothymamo/shift2eu/compare/v0.10.0...v0.10.1) (2026-11-20)


### Bug Fixes

* **aireview:** review a diff over the limit in parts and skip, not fail, what cannot be reviewed ([#367](https://github.com/timothymamo/shift2eu/issues/367), [#376](https://github.com/timothymamo/shift2eu/issues/376)) ([#380](https://github.com/timothymamo/shift2eu/issues/380)) ([47e7951](https://github.com/timothymamo/shift2eu/commit/47e79510ba9aca6bb2aa06fc711297ab7c35ab50))
* **test:** let the chromedp import check run on an empty module cache ([#391](https://github.com/timothymamo/shift2eu/issues/391)) ([#392](https://github.com/timothymamo/shift2eu/issues/392)) ([7b85582](https://github.com/timothymamo/shift2eu/commit/7b8558213257d2cd8b6868754b6c1eedf03747bb))

## [0.10.0](https://github.com/timothymamo/shift2eu/compare/v0.9.0...v0.10.0) (2026-11-20)


### Features

* **site:** the site's pages, CSP, Trusted Types and a browser smoke test (PRD [#297](https://github.com/timothymamo/shift2eu/issues/297) M2) ([#359](https://github.com/timothymamo/shift2eu/issues/359)) ([54f121f](https://github.com/timothymamo/shift2eu/commit/54f121f9dd5dd444226d61ad73cc9c8e8831b4e7))
* **web:** run the assessment in WebAssembly, with an HTML report (PRD [#297](https://github.com/timothymamo/shift2eu/issues/297) M1) ([#339](https://github.com/timothymamo/shift2eu/issues/339)) ([41243e2](https://github.com/timothymamo/shift2eu/commit/41243e26fa8c24117adaa2e81e20b29b3fccb377))


### Bug Fixes

* **aireview:** keep code names and the decoder's message out of the AI reviewer's findings and log ([#371](https://github.com/timothymamo/shift2eu/issues/371)) ([afc3b9d](https://github.com/timothymamo/shift2eu/commit/afc3b9def62d3e255909ad3367e3a0ecf3e604c4))
* **aireview:** round 3 of the AI reviewer's fixes ([#301](https://github.com/timothymamo/shift2eu/issues/301), [#302](https://github.com/timothymamo/shift2eu/issues/302), [#303](https://github.com/timothymamo/shift2eu/issues/303), [#316](https://github.com/timothymamo/shift2eu/issues/316), [#324](https://github.com/timothymamo/shift2eu/issues/324)) ([#338](https://github.com/timothymamo/shift2eu/issues/338)) ([5c69c78](https://github.com/timothymamo/shift2eu/commit/5c69c78857e15a69552bee65027369ae86bf85fe))
* **aireview:** round 4 of the AI reviewer's fixes ([#341](https://github.com/timothymamo/shift2eu/issues/341), [#342](https://github.com/timothymamo/shift2eu/issues/342), [#343](https://github.com/timothymamo/shift2eu/issues/343), [#344](https://github.com/timothymamo/shift2eu/issues/344)) ([#358](https://github.com/timothymamo/shift2eu/issues/358)) ([0fa2a56](https://github.com/timothymamo/shift2eu/commit/0fa2a56a949e77a7722cc32ea672f8db02956976))
* **devenv:** the agents App key under a separate macOS account ([#227](https://github.com/timothymamo/shift2eu/issues/227)) ([#353](https://github.com/timothymamo/shift2eu/issues/353)) ([edf8d00](https://github.com/timothymamo/shift2eu/commit/edf8d004c43394aa01441e4d340bd16ac497641b))
* **devenv:** token mint part 2 ([#262](https://github.com/timothymamo/shift2eu/issues/262), [#263](https://github.com/timothymamo/shift2eu/issues/263), [#264](https://github.com/timothymamo/shift2eu/issues/264), [#242](https://github.com/timothymamo/shift2eu/issues/242), [#319](https://github.com/timothymamo/shift2eu/issues/319), [#320](https://github.com/timothymamo/shift2eu/issues/320)) ([#333](https://github.com/timothymamo/shift2eu/issues/333)) ([81e6dd5](https://github.com/timothymamo/shift2eu/commit/81e6dd572032b2a29dbfd5c27e734b04477de469))

## [0.9.0](https://github.com/timothymamo/shift2eu/compare/v0.8.1...v0.9.0) (2026-11-19)


### ⚠ BREAKING CHANGES

* **jsonreport:** shift2eu assess --format json now writes schemaVersion 3 by default: a total whose estimated count is 0 has no amount key. Pass --schema-version 2 for the previous document. The HTTP API's /v1/assessments still answers schemaVersion 2 unless the request asks for 3.

### Features

* **jsonreport:** write schemaVersion 3, with no amount when nothing is estimated ([#307](https://github.com/timothymamo/shift2eu/issues/307)) ([#322](https://github.com/timothymamo/shift2eu/issues/322)) ([6a6acc4](https://github.com/timothymamo/shift2eu/commit/6a6acc47a9fc29c913c40b59c646c42a5cb2343e))


### Bug Fixes

* **devenv:** keep one refresher on a contested stale lock and kill the sleep in the trap window ([#288](https://github.com/timothymamo/shift2eu/issues/288), [#289](https://github.com/timothymamo/shift2eu/issues/289)) ([#317](https://github.com/timothymamo/shift2eu/issues/317)) ([1f82cbf](https://github.com/timothymamo/shift2eu/commit/1f82cbf07d61483cf1f94686ab5d2b6646b69014))
* **devenv:** run every host target only from the host clone ([#227](https://github.com/timothymamo/shift2eu/issues/227) part 1) ([#323](https://github.com/timothymamo/shift2eu/issues/323)) ([e8b8df1](https://github.com/timothymamo/shift2eu/commit/e8b8df129c6b59e14713d2e70eefbfb439fa6015))
* **scaleway:** list Kubernetes Kapsule Dedicated in it-mil ([#325](https://github.com/timothymamo/shift2eu/issues/325)) ([5f77772](https://github.com/timothymamo/shift2eu/commit/5f77772a26c1e82d082b1c0de2e9b149ea49abaa))

## [0.8.1](https://github.com/timothymamo/shift2eu/compare/v0.8.0...v0.8.1) (2026-11-19)


### Bug Fixes

* **aireview:** keep a review with one bad category, count findings plainly, and pin the CLI outcome ([#295](https://github.com/timothymamo/shift2eu/issues/295)) ([f645a30](https://github.com/timothymamo/shift2eu/commit/f645a30e5cfb552cb54f38316e7eea0ef147bb84))
* **devenv:** check LIMA_INSTANCE on every target and start an existing VM without template:default ([#252](https://github.com/timothymamo/shift2eu/issues/252), [#254](https://github.com/timothymamo/shift2eu/issues/254)) ([#312](https://github.com/timothymamo/shift2eu/issues/312)) ([f853a5e](https://github.com/timothymamo/shift2eu/commit/f853a5e302b34b13084a528a5fd2b1b84fbdd0bc))
* **devenv:** harden vm-env and vm-env-check ([#268](https://github.com/timothymamo/shift2eu/issues/268), [#259](https://github.com/timothymamo/shift2eu/issues/259)) ([#296](https://github.com/timothymamo/shift2eu/issues/296)) ([ff3a754](https://github.com/timothymamo/shift2eu/commit/ff3a754bfb3aba206cdc6bfc4f46fcf8753c2161))
* **estimate:** say "not offered in &lt;region&gt;", and print no amount when nothing is estimated ([#299](https://github.com/timothymamo/shift2eu/issues/299)) ([1268c6c](https://github.com/timothymamo/shift2eu/commit/1268c6c32218a4a366efec6285374be1a59e4363))

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
