#!/bin/sh
# Prints the script make vm-agents-token-ssh runs in the Lima VM over limactl's standard input (issue #227 part
# 2, docs/adr/0031-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md, update of
# 2026-11-19, 4): it lets the separate account's SSH key run one root-owned writer, and nothing else, so the
# account's refresh can write the agents App token into the VM over the VM's pinned SSH port.
#
# Usage: dev/vm-agents-token-ssh.sh <writer file> <public key line> <vm user> | limactl shell --workdir / <instance> sh -s
#        dev/vm-agents-token-ssh.sh --uninstall <vm user> | limactl shell --workdir / <instance> sh -s
# (make vm-agents-token-ssh and make vm-agents-token-ssh-uninstall generate the script first and send it only
# when it is whole.) The writer file is what `dev/agents-app-token.sh vm-writer` prints.
#
# It refuses, before it prints anything: a public key that is not one line `ssh-ed25519 <base64>` with an
# optional comment of letters, digits and @ . _ - (no options, quotes, commas or second line can reach the keys
# file the VM's sshd reads; the comment is replaced with shift2eu-agents-token); a VM user that is not letters,
# digits, ., _ and -, beginning with a letter, digit or _; a writer that is no file, does not start with
# #!/bin/sh, or holds the line that ends the here-document it travels in.
#
# The printed script is one function, called on its last line, so a stream cut short runs nothing. Run as the
# VM user (who has passwordless sudo), vm_agents_token_ssh:
#   1. writes the writer to a temporary file and refuses it unless sh -n parses it, then installs it, root-owned
#      0755, as /usr/local/libexec/shift2eu/.agents-token-write.new and moves it over
#      /usr/local/libexec/shift2eu/agents-token-write;
#   2. writes /etc/ssh/shift2eu-agents-token/<vm user>.keys (root, 0644, in a root 0755 directory), holding
#      the one line restrict,command="<writer> /usr/local/libexec/shift2eu/gh" ssh-ed25519 <base64>
#      shift2eu-agents-token, and the sshd drop-in /etc/ssh/sshd_config.d/50-shift2eu-agents-token.conf, holding
#      AuthorizedKeysFile .ssh/authorized_keys .ssh/authorized_keys2 /etc/ssh/shift2eu-agents-token/%u.keys,
#      each through a .new file beside it and mv;
#   3. runs sshd -t and, only when that passes, systemctl try-reload-or-restart ssh.service, which reloads a
#      running sshd and leaves an inactive one (Ubuntu's ssh.socket starts it at the next connection, reading
#      the new configuration) instead of failing as reload does; on a failing sshd -t it removes the drop-in
#      again and fails;
#   4. prints the keys line it wrote, the VM's host key as host-key=ssh-ed25519 <base64>, and
#      "vm-agents-token-ssh: done", which the make recipe requires.
# vm_agents_token_ssh_uninstall removes the drop-in, the keys file and the writer, reloads sshd once sshd -t
# passes, and prints "vm-agents-token-ssh-uninstall: done". Both are safe to run again. Nothing secret travels:
# the public key and the writer's text only.
set -eu

die() {
  printf 'vm-agents-token-ssh: %s\n' "$*" >&2
  exit 1
}

# vm_user <name> refuses a VM user name that could be anything but a user name in a path and a sudo argument.
check_vm_user() {
  case "$1" in
  '' | [!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_]* | *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*)
    die "the VM user (SHIFT2EU_AGENTS_VM_SSH_USER, default your account name) must be letters, digits, ., _ and -, beginning with a letter, digit or _"
    ;;
  esac
}

if [ "${1:-}" = --uninstall ]; then
  [ "$#" -eq 2 ] || die "usage: $0 --uninstall <vm user>"
  check_vm_user "$2"
  cat <<'UNINSTALL'
vm_agents_token_ssh_uninstall() {
  set -eu
UNINSTALL
  printf "  keys=/etc/ssh/shift2eu-agents-token/%s.keys\n" "$2"
  cat <<'UNINSTALL'
  dropin=/etc/ssh/sshd_config.d/50-shift2eu-agents-token.conf
  sudo rm -f "${dropin}" "${dropin}.new" "${keys}" "${keys}.new" \
    /usr/local/libexec/shift2eu/agents-token-write /usr/local/libexec/shift2eu/.agents-token-write.new
  sudo rmdir /etc/ssh/shift2eu-agents-token 2>/dev/null || true
  if ! sudo sshd -t; then
    echo "vm-agents-token-ssh-uninstall: sshd -t fails with the drop-in removed: fix /etc/ssh/sshd_config in the VM; sshd was not reloaded" >&2
    return 1
  fi
  sudo systemctl try-reload-or-restart ssh.service
  echo "vm-agents-token-ssh-uninstall: done"
}
vm_agents_token_ssh_uninstall
UNINSTALL
  exit 0
fi

[ "$#" -eq 3 ] || die "usage: $0 <writer file> <public key line> <vm user>"
writer="$1"
pub="$2"
check_vm_user "$3"

nl="$(printf '\nx')"
nl="${nl%x}"
case "${pub}" in
*"${nl}"*) die "the account's public key file holds more than one line: it must hold one line, ssh-ed25519 <base64> [comment]" ;;
esac
# The key type, one space, base64, and an optional comment of letters, digits and @ . _ -: nothing that could
# be an option, a quote or a second key reaches the keys file. Spelled out, not A-Z: some shells match a range by
# the locale's collation (dev/agents-app-token.sh).
if ! printf '%s\n' "${pub}" | grep -Eqx 'ssh-ed25519 [ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/]+={0,2}( [ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789@._-]+)?'; then
  die "the account's public key is not one line ssh-ed25519 <base64> with an optional comment of letters, digits and @ . _ -: generate it as the account with ssh-keygen -t ed25519 (docs/dev-environment.md, The agents App); nothing sent to the VM"
fi
key="$(printf '%s\n' "${pub}" | cut -d ' ' -f 1,2)"
keys_line="restrict,command=\"/usr/local/libexec/shift2eu/agents-token-write /usr/local/libexec/shift2eu/gh\" ${key} shift2eu-agents-token"

[ -f "${writer}" ] || die "${writer} is not a file: it must hold what dev/agents-app-token.sh vm-writer prints"
[ "$(sed -n 1p "${writer}")" = '#!/bin/sh' ] || die "${writer} does not start with #!/bin/sh: it is not the writer dev/agents-app-token.sh vm-writer prints"
# The writer ends the here-document it travels in only at its delimiter: refuse one that holds that line.
if grep -qx 'SHIFT2EU_AGENTS_TOKEN_WRITE' "${writer}"; then
  die "${writer} holds the line SHIFT2EU_AGENTS_TOKEN_WRITE, which would end the writer early"
fi

cat <<'INSTALL'
vm_agents_token_ssh() {
  set -eu
  libexec=/usr/local/libexec/shift2eu
  keys_dir=/etc/ssh/shift2eu-agents-token
  dropin=/etc/ssh/sshd_config.d/50-shift2eu-agents-token.conf
INSTALL
printf "  keys=\"\${keys_dir}/%s.keys\"\n" "$3"
printf "  keys_line='%s'\n" "${keys_line}"
cat <<'INSTALL'
  tmp="$(mktemp)"
  trap 'rm -f "${tmp}"' EXIT
  cat >"${tmp}" <<'SHIFT2EU_AGENTS_TOKEN_WRITE'
INSTALL
cat "${writer}"
cat <<'INSTALL'
SHIFT2EU_AGENTS_TOKEN_WRITE
  if ! sh -n "${tmp}"; then
    echo "vm-agents-token-ssh: the writer does not parse; nothing installed" >&2
    return 1
  fi
  sudo install -d -o root -g root -m 0755 "${libexec}"
  sudo install -o root -g root -m 0755 "${tmp}" "${libexec}/.agents-token-write.new"
  sudo mv -f "${libexec}/.agents-token-write.new" "${libexec}/agents-token-write"
  sudo install -d -o root -g root -m 0755 "${keys_dir}"
  printf '%s\n' "${keys_line}" >"${tmp}"
  sudo install -o root -g root -m 0644 "${tmp}" "${keys}.new"
  sudo mv -f "${keys}.new" "${keys}"
  printf '%s\n' 'AuthorizedKeysFile .ssh/authorized_keys .ssh/authorized_keys2 /etc/ssh/shift2eu-agents-token/%u.keys' >"${tmp}"
  sudo install -o root -g root -m 0644 "${tmp}" "${dropin}.new"
  sudo mv -f "${dropin}.new" "${dropin}"
  if ! sudo sshd -t; then
    sudo rm -f "${dropin}"
    echo "vm-agents-token-ssh: sshd -t refuses the configuration with the drop-in ${dropin}, so it was removed again and sshd was not reloaded" >&2
    return 1
  fi
  sudo systemctl try-reload-or-restart ssh.service
  printf 'vm-agents-token-ssh: %s holds: %s\n' "${keys}" "${keys_line}"
  printf 'host-key=%s\n' "$(cut -d ' ' -f 1,2 /etc/ssh/ssh_host_ed25519_key.pub)"
  echo "vm-agents-token-ssh: done"
}
vm_agents_token_ssh
INSTALL
