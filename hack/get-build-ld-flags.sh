#!/usr/bin/env bash

set -e

PACKAGE_PATH="${1:-k8s.io/component-base}"
VERSION_PATH="${2:-$(dirname $0)/../VERSION}"
PROGRAM_NAME="${3:-Endpoint_Copier_Operator}"
VERSION_VERSIONFILE="$(cat "$VERSION_PATH")"
VERSION="${EFFECTIVE_VERSION:-$VERSION_VERSIONFILE}"

MAJOR_VERSION=""
MINOR_VERSION=""

if [[ "${VERSION}" =~ ^v([0-9]+)\.([0-9]+)(\.[0-9]+)?([-].*)?([+].*)?$ ]]; then
  MAJOR_VERSION=${BASH_REMATCH[1]}
  MINOR_VERSION=${BASH_REMATCH[2]}
  if [[ -n "${BASH_REMATCH[4]}" ]]; then
    MINOR_VERSION+="+"
  fi
fi

TREE_STATE="$([ -z "$(git status --porcelain 2>/dev/null | grep -vf <(git ls-files -o --deleted --ignored --exclude-from=.dockerignore) -e 'VERSION')" ] && echo clean || echo dirty)"

echo "-X $PACKAGE_PATH/version.gitMajor=$MAJOR_VERSION
      -X $PACKAGE_PATH/version.gitMinor=$MINOR_VERSION
      -X $PACKAGE_PATH/version.gitVersion=$VERSION
      -X $PACKAGE_PATH/version.gitTreeState=$TREE_STATE
      -X $PACKAGE_PATH/version.gitCommit=$(git rev-parse --verify HEAD)
      -X $PACKAGE_PATH/version.buildDate=$(date '+%Y-%m-%dT%H:%M:%S%z' | sed 's/\([0-9][0-9]\)$/:\1/g')
      -X $PACKAGE_PATH/version/verflag.programName=$PROGRAM_NAME"