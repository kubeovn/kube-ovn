#!/bin/bash

# Kube-OVN Backup Script
# 
# This script downloads and backs up Kube-OVN components for a specified version:
# 1. Docker images for both amd64 and arm64 architectures
# 2. Source code from the main repository
# 3. Documentation from the docs repository
#
# Usage:
#   ./backup.sh <version>
#
# Examples:
#   ./backup.sh v1.14.5
#   ./backup.sh v1.13.2
#
# Requirements:
#   - Docker installed and running
#   - curl command available
#   - Internet connection
#
# Output Structure:
#   backup_<version>_<timestamp>/
#   ├── images/
#   │   ├── amd64/              # AMD64 architecture images
#   │   │   ├── kube-ovn-base_<version>-amd64.tar
#   │   │   ├── kube-ovn_<version>-amd64.tar
#   │   │   └── vpc-nat-gateway_<version>-amd64.tar
#   │   └── arm64/              # ARM64 architecture images
#   │       ├── kube-ovn-base_<version>-arm64.tar
#   │       ├── kube-ovn_<version>-arm64.tar
#   │       └── vpc-nat-gateway_<version>-arm64.tar
#   ├── source/                 # Main project source code
#   │   └── kube-ovn-<version>.tar.gz
#   └── docs/                   # Documentation source code
#       └── kube-ovn-docs-<branch>.tar.gz
#
# Notes:
#   - Images are downloaded using docker pull --platform to get specific architectures
#   - Documentation branch is automatically determined from version (e.g., v1.14.5 -> v1.14)
#   - Script will continue if documentation download fails (with warning)
#   - All operations are logged with timestamps

set -euo pipefail

VERSION=""
BACKUP_DIR=""
DOCKER_REGISTRY="kubeovn"
IMAGES=(
    "kube-ovn-base"
    "kube-ovn"
    "vpc-nat-gateway"
)
ARCHITECTURES=(
    "amd64"
    "arm64"
)

usage() {
    echo "Usage: $0 <version>"
    echo "Example: $0 v1.12.0"
    exit 1
}

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

check_docker() {
    if ! command -v docker &> /dev/null; then
        log "ERROR: Docker is not installed or not in PATH"
        exit 1
    fi
    
    if ! docker info &> /dev/null; then
        log "ERROR: Docker daemon is not running"
        exit 1
    fi
}

validate_version() {
    local version=$1
    if [[ ! $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        log "ERROR: Invalid version format. Expected format: v1.12.0"
        exit 1
    fi
}

create_backup_dir() {
    local timestamp=$(date '+%Y%m%d_%H%M%S')
    BACKUP_DIR="backup_${VERSION}_${timestamp}"
    mkdir -p "$BACKUP_DIR"/{images/{amd64,arm64},source,docs}
    log "Created backup directory: $BACKUP_DIR"
}

download_and_export_images() {
    log "Starting to download and export Docker images for version $VERSION"
    
    local failed_operations=()
    
    for image in "${IMAGES[@]}"; do
        for arch in "${ARCHITECTURES[@]}"; do
            local full_image="$DOCKER_REGISTRY/$image:$VERSION"
            local platform="linux/$arch"
            local tar_file="$BACKUP_DIR/images/$arch/${image}_${VERSION}-${arch}.tar"
            
            log "Downloading image: $full_image for platform $platform"
            
            if docker pull --platform "$platform" "$full_image"; then
                log "Successfully downloaded: $full_image ($platform)"
                
                log "Exporting image: $full_image ($platform)"
                if docker save -o "$tar_file" "$full_image"; then
                    log "Successfully exported: $tar_file"
                else
                    log "ERROR: Failed to export: $full_image ($platform)"
                    failed_operations+=("Export: $full_image ($platform)")
                fi
            else
                log "WARNING: Failed to download: $full_image ($platform)"
                failed_operations+=("Download: $full_image ($platform)")
            fi
        done
    done
    
    if [ ${#failed_operations[@]} -gt 0 ]; then
        log "WARNING: The following operations failed:"
        printf '%s\n' "${failed_operations[@]}"
    fi
}



get_docs_branch() {
    local version=$1
    # Extract major.minor from version (e.g., v1.14.5 -> v1.14)
    echo "$version" | sed -E 's/^(v[0-9]+\.[0-9]+)\.[0-9]+$/\1/'
}

download_source() {
    log "Downloading source code for version $VERSION"
    
    # Download main kube-ovn source code
    local source_url="https://github.com/kubeovn/kube-ovn/archive/refs/tags/${VERSION}.tar.gz"
    local source_file="$BACKUP_DIR/source/kube-ovn-${VERSION}.tar.gz"
    
    if curl -L -o "$source_file" "$source_url"; then
        log "Successfully downloaded source code: $source_file"
    else
        log "ERROR: Failed to download source code from: $source_url"
        exit 1
    fi
    
    # Download documentation source code
    local docs_branch=$(get_docs_branch "$VERSION")
    local docs_url="https://github.com/kubeovn/docs/archive/refs/heads/${docs_branch}.tar.gz"
    local docs_file="$BACKUP_DIR/docs/kube-ovn-docs-${docs_branch}.tar.gz"
    
    log "Downloading documentation for branch $docs_branch"
    
    if curl -L -o "$docs_file" "$docs_url"; then
        log "Successfully downloaded documentation: $docs_file"
    else
        log "WARNING: Failed to download documentation from: $docs_url"
        log "Documentation branch $docs_branch may not exist"
    fi
}

main() {
    if [ $# -ne 1 ]; then
        usage
    fi
    
    VERSION=$1
    
    log "Starting backup process for kube-ovn version: $VERSION"
    
    validate_version "$VERSION"
    check_docker
    create_backup_dir
    download_and_export_images
    download_source
    
    log "Backup completed successfully!"
    log "Backup location: $BACKUP_DIR"
    log "Contents:"
    find "$BACKUP_DIR" -type f -exec ls -lh {} \;
}

main "$@"
