package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"slices"
)

// maxPageBytes bounds the live page's body the guard reads. A page over it
// is no release's (the site's pages are a few KiB), so it is untrusted.
const maxPageBytes = 1 << 20

// verdict is the rollback guard's reading of the live page.
type verdict struct {
	// refuse reports whether the live page is a genuine higher release.
	refuse bool
	// genuine reports whether the live page is a release not higher than
	// the bundle's that matches its manifest: a plain notice, no warning.
	genuine bool
	// version is the live page's release tag, "" when it shows none that
	// can be trusted to be one.
	version string
	// reason says, for a warning, why the live page is not trusted.
	reason string
}

// guard runs the rollback guard (doc.go, step 2): one unsigned GET of the
// live page, following no redirect. It returns ErrRollback only for a
// genuine higher release, and otherwise writes a notice or a warning to the
// summary and lets the deploy overwrite.
func (r *run) guard(ctx context.Context) error {
	v := r.readLivePage(ctx)

	switch {
	case v.refuse:
		r.summaryf("> [!CAUTION]\n> Refused to deploy %s: the live site is release %s, which is higher and matches its manifest. Nothing was uploaded.\n\n",
			r.cfg.Version, v.version)

		return r.fail(ErrRollback, "the live site is release %s, higher than %s, and matches its manifest", v.version, r.cfg.Version)
	case v.genuine:
		r.summaryf("> [!NOTE]\n> The deploy of %s overwrites the live site, release %s, which matches its manifest.\n\n",
			r.cfg.Version, v.version)
	case v.version != "":
		r.summaryf("> [!WARNING]\n> The deploy of %s overwrites a live page that claims %s, which is not trusted: %s.\n\n",
			r.cfg.Version, v.version, v.reason)
	default:
		r.summaryf("> [!WARNING]\n> The deploy of %s overwrites a live page whose version is unknown: %s.\n\n",
			r.cfg.Version, v.reason)
	}

	return nil
}

// readLivePage reads the live page once and decides what it is. The page's
// text never becomes a path or a tag: the tag and the manifest's name are
// taken from Config.Releases.
func (r *run) readLivePage(ctx context.Context) verdict {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.cfg.LivePage, http.NoBody)
	if err != nil {
		r.logf("GET %s failed", r.cfg.LivePage)

		return verdict{reason: "the live page could not be requested"}
	}

	resp, err := r.client.Do(req)
	if err != nil {
		r.logf("GET %s failed", r.cfg.LivePage)

		return verdict{reason: fmt.Sprintf("the live page could not be read (%v)", err)}
	}

	defer func() { _ = resp.Body.Close() }()

	r.logf("GET %s %d", r.cfg.LivePage, resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return verdict{reason: fmt.Sprintf("the live page answered HTTP %d, not 200 (no redirect is followed)", resp.StatusCode)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return verdict{reason: "the live page's body could not be read"}
	}

	if len(body) > maxPageBytes {
		return verdict{reason: fmt.Sprintf("the live page is over %d bytes", maxPageBytes)}
	}

	claimed, err := LiveVersion(body)
	if err != nil {
		return verdict{reason: "the live page shows no release version it can read"}
	}

	index := slices.Index(r.cfg.Releases, claimed)
	if index < 0 {
		return verdict{version: claimed, reason: "it is no release tag on main"}
	}

	tag := r.cfg.Releases[index]

	m, err := r.releaseManifest(tag)
	if err != nil {
		return verdict{version: tag, reason: err.Error()}
	}

	want, ok := m.lookup("index.html")
	if !ok {
		return verdict{version: tag, reason: "the manifest of " + tag + " names no index.html"}
	}

	if HashPayload(body) != want {
		return verdict{version: tag, reason: "the page is not the index.html of that release's manifest, so a leaked key could have written it"}
	}

	if compareTags(tag, r.cfg.Version) > 0 {
		return verdict{refuse: true, version: tag}
	}

	return verdict{genuine: true, version: tag}
}

// releaseManifest reads "<tag>.json" of Config.Manifests, tag a tag of
// Config.Releases. Its error, errNoManifest when there is no such file, is a
// reason for the summary: the manifest does not parse or names another
// version. Its text never quotes the manifest.
func (r *run) releaseManifest(tag string) (Manifest, error) {
	data, err := readBounded(r.cfg.Manifests, tag+".json", maxManifestBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, fmt.Errorf("release %s %w", tag, errNoManifest)
	}

	if err != nil {
		return Manifest{}, fmt.Errorf("the manifest of %s cannot be read", tag)
	}

	m, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, fmt.Errorf("the manifest of %s does not parse", tag)
	}

	if m.Version != tag {
		return Manifest{}, fmt.Errorf("the manifest of %s names another version", tag)
	}

	return m, nil
}

// errNoManifest is releaseManifest's error for a tag with no manifest.
var errNoManifest = errors.New("has no manifest")
