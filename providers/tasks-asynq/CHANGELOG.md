# Changelog

## 0.1.0 (2026-11-11)


### Added

* **tasks:** the asynq provider is installable as providers/tasks-asynq, a module that forwards to the in-tree package until v2.0.0 (DEP-2026-020) ([#651](https://github.com/jcsvwinston/nucleus/issues/651)) ([1cfc252](https://github.com/jcsvwinston/nucleus/commit/1cfc252338b9287eaee02cba53f33f646f3de7f3))


### Fixed

* **deps:** build and test with Go 1.26.11, which fixes GO-2026-6859 in net/http ([#653](https://github.com/jcsvwinston/nucleus/issues/653)) ([c803355](https://github.com/jcsvwinston/nucleus/commit/c803355230f70b6b19ee10430960dcc62d6ac59d))
* **storage:** corta v1.9.2 — el contrato de EnsureBucket ya no miente (QCD-FW-12) ([#272](https://github.com/jcsvwinston/nucleus/issues/272)) ([1938e71](https://github.com/jcsvwinston/nucleus/commit/1938e71ed1496c38f4df6fc0e78a9e86b10eb3fa))


### Chore

* **release:** corta v1.9.2 — QCD-FW-12 (doc + contrato, sin cambio de runtime) ([50c4ace](https://github.com/jcsvwinston/nucleus/commit/50c4ace0888a3b176a14f3a7424b3accdc7f52dd))
