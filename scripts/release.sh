#!/usr/bin/env bash
# scripts/release.sh - build a multi-platform release locally and publish.
#
# What it does:
#   1. Verifies clean working tree, HEAD on a tag.
#   2. Cross-compiles gripper for darwin-{arm64,amd64} + linux-{arm64,amd64}.
#   3. Packages each into dist/gripper_<version>_<os>_<arch>.tar.gz alongside
#      gripper-fzf, LICENSE, README.md.
#   4. Creates or updates the GitHub release for the tag and uploads the
#      tarballs.
#   5. Rewrites Formula/gripper.rb in DanielePessina/homebrew-tap to point at
#      the precompiled tarballs (no `go => :build` dep).
#   6. Pushes the tap.
#
# Run from the gripper repo root, on a tagged commit. Requires gh (authenticated),
# go, and git. Works with the bash 3.2 that ships on macOS.
#
# Typical usage:
#   git tag v0.2.0
#   git push origin v0.2.0
#   scripts/release.sh

set -euo pipefail

REPO="DanielePessina/gripper"
TAP_REPO="DanielePessina/homebrew-tap"

PLATFORMS=(
  "darwin/arm64"
  "darwin/amd64"
  "linux/arm64"
  "linux/amd64"
)

# ---------- preflight ----------

for tool in go gh git tar shasum mktemp; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "Error: missing required tool '$tool'." >&2
    exit 1
  }
done

if ! gh auth status >/dev/null 2>&1; then
  echo "Error: gh not authenticated. Run: gh auth login" >&2
  exit 1
fi

if [[ ! -f cmd/gripper-tui/main.go ]]; then
  echo "Error: run this script from the gripper repo root." >&2
  exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
  echo "Error: working tree is dirty. Commit or stash first." >&2
  git status --short >&2
  exit 1
fi

TAG=$(git describe --exact-match --tags HEAD 2>/dev/null) || {
  echo "Error: HEAD is not on a tag. Tag first (e.g. git tag v0.2.0)." >&2
  exit 1
}
VERSION="${TAG#v}"

# Ensure the tag exists on the remote so `gh release` can attach to it.
if ! git ls-remote --tags origin "$TAG" 2>/dev/null | grep -q "refs/tags/$TAG"; then
  echo "Pushing tag $TAG to origin..."
  git push origin "$TAG"
fi

echo "Releasing $REPO @ $TAG (version $VERSION)"
echo

# ---------- build ----------

DIST=dist
rm -rf "$DIST"
mkdir -p "$DIST"

SHA_DARWIN_ARM64=""
SHA_DARWIN_AMD64=""
SHA_LINUX_ARM64=""
SHA_LINUX_AMD64=""

for plat in "${PLATFORMS[@]}"; do
  GOOS="${plat%/*}"
  GOARCH="${plat#*/}"
  PLATKEY="${GOOS}_${GOARCH}"

  echo "Building $GOOS/$GOARCH..."

  STAGE="$DIST/stage_${PLATKEY}"
  mkdir -p "$STAGE"

  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags "-s -w" -o "$STAGE/gripper" ./cmd/gripper-tui

  cp bin/gripper-fzf "$STAGE/gripper-fzf"
  chmod +x "$STAGE/gripper-fzf"
  cp LICENSE README.md "$STAGE/"

  TARBALL="$DIST/gripper_${VERSION}_${PLATKEY}.tar.gz"
  tar -czf "$TARBALL" -C "$STAGE" .

  SHA=$(shasum -a 256 "$TARBALL" | awk '{print $1}')
  case "$plat" in
    darwin/arm64) SHA_DARWIN_ARM64="$SHA" ;;
    darwin/amd64) SHA_DARWIN_AMD64="$SHA" ;;
    linux/arm64)  SHA_LINUX_ARM64="$SHA" ;;
    linux/amd64)  SHA_LINUX_AMD64="$SHA" ;;
  esac
  echo "  -> $(basename "$TARBALL") ($SHA)"
done

echo

# ---------- release ----------

NOTES="$DIST/notes.md"
cat > "$NOTES" <<EOF
\`gripper $TAG\` - precompiled binaries.

Install via Homebrew:
\`\`\`
brew install DanielePessina/tap/gripper
\`\`\`

Or download the appropriate tarball below, extract it, and place \`gripper\`
and \`gripper-fzf\` somewhere on your \$PATH.
EOF

if gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
  echo "Release $TAG already exists; re-uploading assets."
else
  echo "Creating release $TAG..."
  gh release create "$TAG" --repo "$REPO" --title "$TAG" --notes-file "$NOTES"
fi

for plat in "${PLATFORMS[@]}"; do
  PLATKEY="${plat%/*}_${plat#*/}"
  TARBALL="$DIST/gripper_${VERSION}_${PLATKEY}.tar.gz"
  gh release upload "$TAG" --repo "$REPO" "$TARBALL" --clobber
done

echo

# ---------- update tap ----------

echo "Updating tap formula in $TAP_REPO..."

TAP_DIR=$(mktemp -d -t gripper-tap.XXXXXX)
cleanup() { rm -rf "$TAP_DIR"; }
trap cleanup EXIT

gh repo clone "$TAP_REPO" "$TAP_DIR/tap" -- --quiet

cat > "$TAP_DIR/tap/Formula/gripper.rb" <<EOF
class Gripper < Formula
  desc "Interactive picker for selectively downloading files from GitHub repos"
  homepage "https://github.com/$REPO"
  version "$VERSION"
  license "MIT"

  depends_on "fzf"
  depends_on "gh"
  depends_on "jq"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/$REPO/releases/download/$TAG/gripper_${VERSION}_darwin_arm64.tar.gz"
      sha256 "$SHA_DARWIN_ARM64"
    else
      url "https://github.com/$REPO/releases/download/$TAG/gripper_${VERSION}_darwin_amd64.tar.gz"
      sha256 "$SHA_DARWIN_AMD64"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/$REPO/releases/download/$TAG/gripper_${VERSION}_linux_arm64.tar.gz"
      sha256 "$SHA_LINUX_ARM64"
    else
      url "https://github.com/$REPO/releases/download/$TAG/gripper_${VERSION}_linux_amd64.tar.gz"
      sha256 "$SHA_LINUX_AMD64"
    end
  end

  def install
    bin.install "gripper"
    bin.install "gripper-fzf"
  end

  test do
    assert_match "Usage", shell_output("#{bin}/gripper --help")
    assert_match "Usage", shell_output("#{bin}/gripper-fzf --help")
  end
end
EOF

cd "$TAP_DIR/tap"
if [[ -z "$(git status --porcelain)" ]]; then
  echo "Tap formula already at $TAG; nothing to push."
else
  git add Formula/gripper.rb
  git commit -m "Update gripper to $TAG"
  git push origin main
  echo "Tap formula updated."
fi

cd - >/dev/null

echo
echo "Done."
echo "  Release : https://github.com/$REPO/releases/tag/$TAG"
echo "  Formula : https://github.com/$TAP_REPO/blob/main/Formula/gripper.rb"
echo
echo "Verify on a fresh machine:"
echo "  brew update && brew reinstall DanielePessina/tap/gripper"
