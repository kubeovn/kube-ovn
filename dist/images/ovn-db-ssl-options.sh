#!/bin/bash

function ovn_db_tls_version_index {
  case "$1" in
    1.0 | "TLS 1.0" | TLS10) echo 0 ;;
    1.1 | "TLS 1.1" | TLS11) echo 1 ;;
    1.2 | "TLS 1.2" | TLS12) echo 2 ;;
    1.3 | "TLS 1.3" | TLS13) echo 3 ;;
    *) echo "unsupported TLS version: $1" >&2; return 1 ;;
  esac
}

function ovn_db_ssl_protocols {
  if [[ -z "${TLS_MIN_VERSION:-}" && -z "${TLS_MAX_VERSION:-}" ]]; then
    return 0
  fi

  local protocols=(TLSv1 TLSv1.1 TLSv1.2 TLSv1.3)
  local min=0
  local max=3
  if [[ -n "${TLS_MIN_VERSION:-}" ]]; then
    min=$(ovn_db_tls_version_index "$TLS_MIN_VERSION") || return 1
  fi
  if [[ -n "${TLS_MAX_VERSION:-}" ]]; then
    max=$(ovn_db_tls_version_index "$TLS_MAX_VERSION") || return 1
  fi
  if ((min > max)); then
    echo "TLS_MIN_VERSION ($TLS_MIN_VERSION) must be less than or equal to TLS_MAX_VERSION ($TLS_MAX_VERSION)" >&2
    return 1
  fi

  local selected=()
  for ((i = min; i <= max; i++)); do
    selected+=("${protocols[$i]}")
  done
  local IFS=,
  echo "${selected[*]}"
}

function ovn_db_cipher_suite_args {
  if [[ -z "${TLS_CIPHER_SUITES:-}" ]]; then
    return 0
  fi

  local ciphers=()
  local ciphersuites=()
  local suites=()
  IFS=, read -ra suites <<< "$TLS_CIPHER_SUITES"
  for suite in "${suites[@]}"; do
    suite="${suite#"${suite%%[![:space:]]*}"}"
    suite="${suite%"${suite##*[![:space:]]}"}"
    case "$suite" in
      TLS_AES_128_GCM_SHA256 | TLS_AES_256_GCM_SHA384 | TLS_CHACHA20_POLY1305_SHA256)
        ciphersuites+=("$suite")
        ;;
      TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA)
        ciphers+=(ECDHE-ECDSA-AES128-SHA)
        ;;
      TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA)
        ciphers+=(ECDHE-ECDSA-AES256-SHA)
        ;;
      TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA)
        ciphers+=(ECDHE-RSA-AES128-SHA)
        ;;
      TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA)
        ciphers+=(ECDHE-RSA-AES256-SHA)
        ;;
      TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256)
        ciphers+=(ECDHE-ECDSA-AES128-GCM-SHA256)
        ;;
      TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384)
        ciphers+=(ECDHE-ECDSA-AES256-GCM-SHA384)
        ;;
      TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)
        ciphers+=(ECDHE-RSA-AES128-GCM-SHA256)
        ;;
      TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384)
        ciphers+=(ECDHE-RSA-AES256-GCM-SHA384)
        ;;
      TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256)
        ciphers+=(ECDHE-RSA-CHACHA20-POLY1305)
        ;;
      TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256)
        ciphers+=(ECDHE-ECDSA-CHACHA20-POLY1305)
        ;;
      "")
        ;;
      *)
        echo "unsupported TLS cipher suite: $suite" >&2
        return 1
        ;;
    esac
  done

  if ((${#ciphers[@]} > 0)); then
    local IFS=:
    echo "--ovn-nb-db-ssl-ciphers=${ciphers[*]}"
    echo "--ovn-sb-db-ssl-ciphers=${ciphers[*]}"
  fi
  if ((${#ciphersuites[@]} > 0)); then
    local IFS=:
    echo "--ovn-nb-db-ssl-ciphersuites=${ciphersuites[*]}"
    echo "--ovn-sb-db-ssl-ciphersuites=${ciphersuites[*]}"
  fi
}

function ovn_db_ssl_args {
  local tls_dir=${1:-/var/run/tls}
  local protocols
  protocols=$(ovn_db_ssl_protocols) || return 1
  local cipher_args
  cipher_args=$(ovn_db_cipher_suite_args) || return 1

  printf "%s\n" \
    "--ovn-nb-db-ssl-key=${tls_dir}/key" \
    "--ovn-nb-db-ssl-cert=${tls_dir}/cert" \
    "--ovn-nb-db-ssl-ca-cert=${tls_dir}/cacert" \
    "--ovn-sb-db-ssl-key=${tls_dir}/key" \
    "--ovn-sb-db-ssl-cert=${tls_dir}/cert" \
    "--ovn-sb-db-ssl-ca-cert=${tls_dir}/cacert" \
    "--ovn-northd-ssl-key=${tls_dir}/key" \
    "--ovn-northd-ssl-cert=${tls_dir}/cert" \
    "--ovn-northd-ssl-ca-cert=${tls_dir}/cacert"

  if [[ -n "$protocols" ]]; then
    echo "--ovn-nb-db-ssl-protocols=$protocols"
    echo "--ovn-sb-db-ssl-protocols=$protocols"
  fi
  if [[ -n "$cipher_args" ]]; then
    echo "$cipher_args"
  fi
}
