#!/bin/bash
# Reconstruct headers and symbol CRCs for the existing MU300 OpenWrt kernel.
# Run in an isolated Linux volume at /build; inputs are mounted read-only.
# No Image or module produced here is installed automatically.
set -euo pipefail
stage=${1:-prepare}
S=/build/zte-u30air
O=/build/out-linux
expected=1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3
config_sum() { grep -E '^(CONFIG_|# CONFIG_)' "$1" | LC_ALL=C sort | sha256sum | cut -d' ' -f1; }
case "$stage" in prepare|compile) ;; *) exit 2 ;; esac
# Offline source excludes only the upstream signing private key.
if [ -f /inputs/zte-b50db5b-preserved.tar.gz ]; then
    kernel_archive=/inputs/zte-b50db5b-preserved.tar.gz
    test "$(sha256sum "$kernel_archive" | cut -d' ' -f1)" = e9ee612dc2f02e020f84608672d2ba05873b61afbc7374b8337a9ed5e261d349
else
    kernel_archive=/inputs/zte-b50db5b.tar.gz
    test "$(sha256sum "$kernel_archive" | cut -d' ' -f1)" = 857c5fd717a7f93668be7df87243f733c8c5b1edc66eda03672c982a43a81ef1
fi
if [ ! -d "$S" ]; then
    mkdir -p "$S"
    tar -xzf "$kernel_archive" --strip-components=1 \
        --exclude='*/certs/signing_key.pem' -C "$S"
    # The release's build-all.sh patch order; source stays isolated from Android work.
    for p in bluetooth-marlin3-link-policy of-reserved-mem-skip regdb-wens-certificate \
        sipa-delegate-einprogress wcn-pcie-scan-timeout sipc-base-addr-attr \
        sipa-delegate-pm-runtime sipc-debug-once sprdwcn-mbuf-pool-deinit-null \
        logspam-quiet sipa-wcn-logspam-quiet wcn-pcie-rx-push-cursor \
        sprdwcn-tx-pending-backpressure xfrm-sysctl-fragment-per-netns sdhci-sprd-dma-ordering; do
        patch --batch --forward --fuzz=0 -d "$S" -p1 < "/linux-reference/kernel/patches/$p.patch"
    done
    printf '%s\n' '-gb50db5b6224c' > "$S/.scmversion"
    touch "$S/.airsim-prepared"
fi
test -f "$S/.airsim-prepared" # Incomplete preparation must not be silently reused.
mkdir -p "$O"
if [ ! -f "$O/.config" ]; then
    cp /linux-reference/kernel/f50-stock-B09.config "$O/.config"
    "$S/scripts/config" --file "$O/.config" --enable THINLTO
    KCONFIG_CONFIG="$O/.config" "$S/scripts/kconfig/merge_config.sh" -m -O "$O" \
        "$O/.config" /linux-reference/kernel/mu300-linux.fragment > /evidence/config-merge.log 2>&1
    make -C "$S" O="$O" ARCH=arm64 LLVM=1 LLVM_IAS=1 CC=clang LD=ld.lld olddefconfig
fi
test "$(config_sum "$O/.config")" = "$expected"
make_args=(-C "$S" O="$O" ARCH=arm64 LLVM=1 LLVM_IAS=1 CC=clang LD=ld.lld -j"${JOBS:-1}")
make "${make_args[@]}" olddefconfig
test "$(config_sum "$O/.config")" = "$expected"
make "${make_args[@]}" modules_prepare
test "$(cat "$O/include/config/kernel.release")" = 5.4.254-gb50db5b6224c
if [ "$stage" = compile ]; then
    # Limit ThinLTO parallelism as well as ordinary compilation.
    make "${make_args[@]}" LDFLAGS_vmlinux=--thinlto-jobs=1 Image modules
    test -s "$O/Module.symvers"
    cp "$O/Module.symvers" /evidence/openwrt-Module.symvers
fi
