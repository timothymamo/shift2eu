#!/usr/bin/env bash
# The dry run of the release's archives (issue #147), run by ci.yml's test job: it builds them with a
# snapshot run of .goreleaser.yaml, which publishes nothing, and fails unless each archive holds exactly
# what it should. The release ships the CLI alone (ADR 0037, issue #393): its archives,
# shift2eu_<version>_<os>_<arch>, hold shift2eu (shift2eu.exe on Windows), and no other archive is
# built. Besides its binary an archive may hold only LICENSE, README.md and SECURITY.md at its root,
# each of which it must hold (issue #450), Markdown pages under docs/ (no path segment may start with a dot, so no ., .. or hidden
# file), and exactly the samples docs/guide/try-it.md runs on (issue #188): examples/README.md and, for
# each of aws, azure and gcp, examples/<source>/README.md and examples/<source>/inventory.json, with
# their directories, and exactly the jq programs docs/guide/inventory.md builds an inventory with (issue
# #178): docs/guide/inventory/<source>.jq for aws, azure and gcp. Any other entry fails the run, so a
# stray file under docs/ (a .jq file there that is not one of the three), or any file under examples/
# that is not a known sample (a real inventory committed as examples/<name>/inventory.json), never ships
# unnoticed. Each archive must hold every sample and jq program too. Every entry must be a regular file
# or a directory: a symbolic link, hard link or device fails the run too. Every built platform has one
# archive, and checksums.txt lists and matches every archive and must pass sha256sum -c in dist/ as a
# whole.
# It then mirrors release.yml's hand-over of the archives to attest-archives (issues #156 and #163): the
# files its patterns pick must pass sha256sum -c checksums.txt, and the subjects they attest must be
# exactly the archives and checksums.txt, which must also be exactly what GoReleaser would upload to the
# release (artifacts.json), or release.yml's publish job would refuse the draft. check.GoReleaserArchives
# pins the configuration itself, and check.GitHubRelease its draft release.
#
# GoReleaser writes into a temporary directory, removed on exit, so the caller's dist/ is left alone.
#
# Usage: .github/scripts/verify-archives.sh   (from the repository root, with goreleaser and jq on PATH)
set -euo pipefail

for tool in goreleaser jq; do
  command -v "${tool}" >/dev/null || { echo "${tool} is not on PATH" >&2; exit 1; }
done

# goreleaser release takes no --dist flag: it builds from a copy of .goreleaser.yaml whose dist: names a
# fresh temporary directory (the original sets no dist:, so its default, dist/, is never touched). Should
# .goreleaser.yaml ever set a top-level dist:, the appended one would duplicate it, so that fails here.
if grep -qE '^dist[[:space:]]*:' .goreleaser.yaml; then
  echo ".goreleaser.yaml sets a top-level dist:, which this dry run replaces: remove it or update this script" >&2
  exit 1
fi
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
dist="${work}/dist"
config="${work}/goreleaser.yaml"
cp .goreleaser.yaml "${config}"
printf '\ndist: %s\n' "${dist}" >>"${config}"
goreleaser release --snapshot --clean --config "${config}" >&2

fail=0
problem() {
  echo "$*" >&2
  fail=1
}

# entries prints the paths an archive holds, one per line.
entries() {
  case "$1" in
    *.tar.gz) tar -tzf "$1" ;;
    *.zip) unzip -Z1 "$1" ;;
    *) echo "$1 is neither .tar.gz nor .zip" >&2; return 1 ;;
  esac
}

# details prints an archive's long listing, one line per entry, each starting with the entry's mode,
# whose first character is its type: - for a regular file, d for a directory, l for a symbolic link, h
# for a hard link (tar) and so on. zipinfo prints a header and a trailer around the entries; the entries
# are its lines that start with a mode, and check_archive holds their count to the number of entries,
# so a listing this misreads fails the run rather than passing it.
details() {
  case "$1" in
    *.tar.gz) tar -tvzf "$1" ;;
    *.zip) unzip -Z -s "$1" | grep -E '^[-a-z?][-rwxsStTlL?]{6,9}[[:space:]]' ;;
    *) echo "$1 is neither .tar.gz nor .zip" >&2; return 1 ;;
  esac
}

# sift runs grep with its arguments but counts status 1, no line selected, as success, so a filter that
# leaves nothing is not an error. Any other failure (status 2 or more: a bad pattern, an unreadable file)
# is passed on, so a check built on it fails closed rather than reading an error as no match.
sift() {
  local status=0
  grep "$@" || status=$?
  if [ "${status}" -gt 1 ]; then
    echo "grep $* failed with status ${status}" >&2
    return "${status}"
  fi
  return 0
}

# check_archive archive own: the archive holds exactly one binary, own (or own.exe), at its root, and
# otherwise only the root files (root_files: LICENSE, README.md, SECURITY.md), Markdown pages under
# docs/, the inventory guide's jq programs (guide_programs) and the samples (with their directories,
# sample_dirs), each a regular file or a directory. No other binary is on that list, so an archive
# holding one fails too. Under examples/ it may hold only the samples, and it must hold every one of
# them, every root file and every jq program: one list, so a file there that is not a known sample
# fails the run.
check_archive() {
  local archive="$1" own="$2" list long special owns stray sample
  list="$(entries "${archive}")" || { problem "${archive}: cannot list its entries"; return; }
  long="$(details "${archive}")" || { problem "${archive}: cannot list its entries' types"; return; }
  if [ "$(wc -l <<<"${long}")" != "$(wc -l <<<"${list}")" ]; then
    problem "${archive}: lists $(wc -l <<<"${list}") entries but the types of $(wc -l <<<"${long}")"
  fi
  special="$(grep -v '^[-d]' <<<"${long}" || true)"
  if [ -n "${special}" ]; then
    problem "${archive}: holds entries that are neither a regular file nor a directory:"$'\n'"${special}"
  fi
  owns="$(grep -cxE "${own}(\\.exe)?" <<<"${list}" || true)"
  if [ "${owns}" != 1 ]; then
    problem "${archive}: holds ${owns} ${own} binaries at its root, want exactly 1"
  fi
  # Both greps run through sift and the pipeline's status is checked, so a grep that fails (status 2,
  # such as an unreadable allow list) fails the run rather than reading as no stray entries.
  if ! stray="$(sift -vxE "${own}(\\.exe)?|docs/([^/.][^/]*/)*[^/.][^/]*\\.md|docs/([^/.][^/]*/)*" <<<"${list}" | sift -vxFf "${allowed}")"; then
    problem "${archive}: cannot match its entries against the allow list (grep failed)"
    return
  fi
  if [ -n "${stray}" ]; then
    problem "${archive}: holds entries outside its allow list:"$'\n'"${stray}"
  fi
  for sample in "${root_files[@]}" "${samples[@]}" "${guide_programs[@]}"; do
    grep -qxF "${sample}" <<<"${list}" \
      || problem "${archive}: does not hold ${sample}, which every archive must ship"
  done
}

# The files at an archive's root beside its binary: every archive must hold each of them, the security
# policy included (issue #450), and no other file may sit there.
root_files=(LICENSE README.md SECURITY.md)
# The samples docs/guide/try-it.md runs on (issue #188): every archive must hold each of them, and
# examples/ in an archive may hold nothing else but their directories (sample_dirs), so a new sample is
# added here as well as to .goreleaser.yaml.
samples=(examples/README.md)
sample_dirs=(examples/)
for source in aws azure gcp; do
  samples+=("examples/${source}/README.md" "examples/${source}/inventory.json")
  sample_dirs+=("examples/${source}/")
done
# The jq programs docs/guide/inventory.md runs (issue #178), which it tells users a release archive
# ships: every archive must hold each of them, and no other non-Markdown file under docs/ is allowed, so
# a new program is added here as well as under docs/guide/inventory/.
guide_programs=()
for source in aws azure gcp; do
  guide_programs+=("docs/guide/inventory/${source}.jq")
done
# The allow list beyond the Markdown pages, one exact path a line, for grep -f: a file, not a process
# substitution, whose status $? would never see.
allowed="${work}/allowed-files"
printf '%s\n' "${root_files[@]}" "${samples[@]}" "${sample_dirs[@]}" "${guide_programs[@]}" >"${allowed}"

shopt -s nullglob
cli=("${dist}"/shift2eu_*.tar.gz "${dist}"/shift2eu_*.zip)

# One archive per built platform: the platforms are those GoReleaser built, read from its own record of
# the run, so a platform dropped from the build (.goreleaser.yaml, releasetarget) still changes the count
# it is held to. The build must have built at least one, and no other build may have built anything.
cli_platforms="$(jq '[.[] | select(.type == "Binary" and .extra.ID == "shift2eu")] | length' "${dist}/artifacts.json")"
other_binaries="$(jq '[.[] | select(.type == "Binary" and .extra.ID != "shift2eu")] | length' "${dist}/artifacts.json")"
[ "${cli_platforms}" -gt 0 ] || problem "GoReleaser built no shift2eu binary"
[ "${other_binaries}" = 0 ] || problem "GoReleaser built ${other_binaries} binaries of another build, want the CLI's alone"
[ "${#cli[@]}" = "${cli_platforms}" ] ||
  problem "${#cli[@]} CLI archives (shift2eu_*), want one per built platform: ${cli_platforms}"

for archive in "${cli[@]}"; do
  check_archive "${archive}" shift2eu
done

# One checksums.txt covers every archive, and every line of it is a file in dist/.
for archive in "${cli[@]}"; do
  grep -qE "  $(basename "${archive}" | sed 's/\./\\./g')$" "${dist}/checksums.txt" ||
    problem "checksums.txt does not list $(basename "${archive}")"
done
(cd "${dist}" && sha256sum --quiet -c checksums.txt) ||
  problem "checksums.txt does not match the archives in dist/"

# The release's hand-over of the archives (issues #156 and #163), on this snapshot: release.yml's
# goreleaser job uploads the files its patterns match, dist/shift2eu_*.tar.gz, dist/shift2eu_*.zip and
# dist/checksums.txt, as the workflow
# artifact archives; attest-archives downloads it, runs sha256sum -c checksums.txt in it and attests
# every file the same patterns match, directories descended into, as actions/upload-artifact and
# actions/attest-build-provenance both expand them. A bare dist/shift2eu_* would also match
# GoReleaser's build directories (dist/shift2eu_linux_amd64_v1/ ...) and hand on their binaries.
# The artifact must pass sha256sum -c, so checksums.txt may list nothing the patterns leave out, and the
# subjects must be exactly the files checksums.txt lists (the archives) and checksums.txt itself: a file
# left unattested, or one attested that the release never ships, fails here and not in a release.
artifact="${work}/artifact"
mkdir "${artifact}"
(cd "${dist}" && cp -R shift2eu_*.tar.gz shift2eu_*.zip checksums.txt "${artifact}/")
(cd "${artifact}" && sha256sum --quiet -c checksums.txt) ||
  problem "checksums.txt does not pass sha256sum -c in the archives artifact release.yml hands to attest-archives"
subjects="$(cd "${artifact}" && find shift2eu_*.tar.gz shift2eu_*.zip checksums.txt -type f | LC_ALL=C sort)"
want="$({ echo checksums.txt; awk '{ print $2 }' "${dist}/checksums.txt"; } | LC_ALL=C sort)"
if [ "${subjects}" != "${want}" ]; then
  problem "attest-archives would attest these files:"$'\n'"${subjects}"$'\n'"want exactly checksums.txt and the files it lists:"$'\n'"${want}"
fi

# What GoReleaser would upload to the draft release, read from its own record of the run: every
# artifact but its run metadata and the bare binaries its builds wrote (named by the binary and its
# extension), which it never uploads. That must be exactly the attested subjects too: the publish job
# refuses a draft holding anything checksums.txt does not list (issue #163), so a new asset registered
# by a pipe a snapshot runs (an SBOM, a signature, a source archive, checksum.extra_files) fails here,
# before a release is left a draft. release.extra_files does not: GoReleaser adds those only in the
# release pipe's publish step, which --snapshot skips, so artifacts.json never lists them; release: is a
# closed section that check.GitHubRelease lets hold none. It fails closed: an artifact type this does
# not know counts as uploaded.
uploads="$(jq -r '.[]
    | select(.type != "Metadata")
    | select((.type == "Binary" and .name == (.extra.Binary + .extra.Ext)) | not)
    | .name' "${dist}/artifacts.json" | LC_ALL=C sort)"
if [ "${uploads}" != "${want}" ]; then
  problem "GoReleaser would upload these files to the release:"$'\n'"${uploads}"$'\n'"want exactly checksums.txt and the files it lists, what attest-archives attests:"$'\n'"${want}"
fi

if [ "${fail}" != 0 ]; then
  exit 1
fi
echo "${#cli[@]} CLI archives, one per built platform, each holding exactly its binary, LICENSE, README.md, SECURITY.md, docs/**/*.md, the inventory guide's jq programs and exactly the samples under examples/, all in checksums.txt"
