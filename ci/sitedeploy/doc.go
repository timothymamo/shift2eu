// Command sitedeploy deploys the site's bundle (PRD #297 milestone 3) to the
// site's Scaleway Object Storage bucket. The release runs it from milestone
// 3's workflow pull request on, in the deploy job, on the bundle that run
// built and attested:
//
//	sitedeploy -bundle <dir> -version <vX.Y.Z> -bucket <name> -region <region>
//	           -releases <file> -manifests <dir> [-list-bucket]
//
// -bundle is the unpacked bundle ci/sitebuild wrote; -version its release
// tag; -bucket and -region the site's bucket and its Object Storage region,
// from which it builds the endpoint https://s3.<region>.scw.cloud (a fixed
// template, so no flag or variable can send the key to another host);
// -releases a file of the repository's release tags on main, one per line, as
// the release's latest job lists them; -manifests a directory holding
// "<tag>.json", the manifest of each earlier release that has a site bundle;
// -list-bucket turns on the listing of the bucket before the upload (off by
// default, ADR 0042: it needs runbook step R7's ListBucket grant).
// The live page the rollback guard reads is the constant livePage.
//
// It reads the key from SCW_ACCESS_KEY and SCW_SECRET_KEY, the two secrets the
// one uploading step's env: holds, and appends its summary to the file
// GITHUB_STEP_SUMMARY names when that is set, through the one os.LookupEnv
// main passes to environment (the forbidigo group site-deploy-env refuses
// every other read of the environment).
// It wires the library deploy (ci/sitedeploy/internal/deploy), which does the
// work, to os.DirFS of both directories and an HTTP client with a timeout, and
// runs deploy.Deploy under a deadline (runDeadline) inside the job's
// timeout-minutes. It exits 0 once every file is uploaded and 1 on any
// failure, with one ::error annotation whose text is escaped; it prints
// neither key.
//
// It is CI tooling, no part of the product: a main package, which nothing can
// import, built by no release archive (.goreleaser.yaml and the Dockerfile
// build cmd/ only), held by the strict depguard rule site-deploy-main to the
// standard packages it imports and its library.
package main
