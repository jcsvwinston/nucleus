# Changelog

## [0.1.2](https://github.com/jcsvwinston/nucleus/compare/providers/auth-saml/v0.1.1...providers/auth-saml/v0.1.2) (2026-11-07)


### Fixed

* **deps:** build and test with Go 1.26.10, which fixes GO-2026-6809 in crypto/x509 ([#635](https://github.com/jcsvwinston/nucleus/issues/635)) ([496a329](https://github.com/jcsvwinston/nucleus/commit/496a32909f910e05d429e509dd2b1369ad0d4dab))
* **deps:** bump github.com/crewjam/saml to v0.5.2, which carries the upstream fix for GO-2026-6909 ([#639](https://github.com/jcsvwinston/nucleus/issues/639)) ([ec950cf](https://github.com/jcsvwinston/nucleus/commit/ec950cf09f6acbcd45897a81a5f9b67cefb55fef))
* **deps:** raise the sibling module floors to nucleus v1.33.0 ([#641](https://github.com/jcsvwinston/nucleus/issues/641)) ([f17772f](https://github.com/jcsvwinston/nucleus/commit/f17772fdae5757be67d7e7a4e036ca30748b8909))

## [0.1.1](https://github.com/jcsvwinston/nucleus/compare/providers/auth-saml/v0.1.0...providers/auth-saml/v0.1.1) (2026-10-18)


### Fixed

* **deps:** raise the sibling module floors to nucleus v1.32.0 ([#632](https://github.com/jcsvwinston/nucleus/issues/632)) ([8cfeca5](https://github.com/jcsvwinston/nucleus/commit/8cfeca58271f23032b82a3b4d40b3d7a4ca8fc00))

## 0.1.0 (2026-10-12)


### Added

* **saml:** a SAML 2.0 service provider as a federated sign-in module, installed and wired by nucleus add saml (A11 N8) ([#602](https://github.com/jcsvwinston/nucleus/issues/602)) ([6624e18](https://github.com/jcsvwinston/nucleus/commit/6624e18ecec84fe900b37da00190cb4a2e41e1ca))


### Fixed

* **deps:** build and test with Go 1.26.9, which fixes GO-2026-6617 in net/http ([#623](https://github.com/jcsvwinston/nucleus/issues/623)) ([1facc34](https://github.com/jcsvwinston/nucleus/commit/1facc348b93593f74a633783725d0e223d77248f))
