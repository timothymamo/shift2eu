# The static rules of the OpenTofu configuration under an infra root, for dev/infra-rules.sh (issue #422).
# It reads every configuration file (*.tf, *.tofu) and test file (*.tftest.hcl, *.tofutest.hcl) given,
# tokenizes each as HCL, and prints one line per broken rule:
#   [<rule>] <file>:<line>: <what is wrong and why>
# and nothing when every rule holds. dev/infra-rules.sh documents the rules; the cases of TestInfraRules
# (test/devenv/infrarules_test.go), one broken rule each, match on the [<rule>] tag.
#
# The tokenizer knows HCL's strings (with ${...} and %{...} templates, which may nest strings), comments
# (#, // and /* */), identifiers, numbers and punctuation. The parser above it tracks only what the rules
# need: blocks with their type and labels, and attributes with their name and whether their value is one
# literal string ("L", its value kept), one identifier such as true ("I", its name kept), one whole number
# ("D", kept), one local.<name> reference ("R", the name kept), an object ("O") or any other expression
# ("E"); a condition's value is also kept as its tokens. Anything it does
# not read is a [parse] failure: a heredoc; an unbalanced string, brace or comment; a backslash in a block
# label or a quoted object key (HCL decodes escapes such as _, so the raw text is not the name tofu
# reads); a $${ or %%{ in a block label (read as ${ and %{); and a { after a block header whose labels
# are not plain identifiers or literal strings. The rules fail closed rather than pass a file they could
# not read: once any file fails to parse, only the [parse] lines are printed, since every other verdict
# would rest on a partial reading.
#
# A call of a function that reads the machine running tofu (file, templatefile, templatestring,
# pathexpand, ...) is found by the tokenizer itself, in a configuration or test file alike: an identifier
# with that name whose next character outside whitespace and comments is a (. Inside a template's ${...}
# or %{...}, which is not tokenized,
# the raw text is read the same way, so the check fails closed there; a string without a template, or a
# comment, is never read for it.
#
# Usage: awk -v pinned=<required_version> -v root=<infra root> -f dev/infra-rules.awk <files...>
# POSIX awk only: no gawk extension, and character classes are written as explicit comparisons where a
# bracket expression's edge cases (a ] or [ inside it) differ between awks. It runs and is tested in the
# VM and in CI (gawk on both, through TestInfraRules) and was also run under mawk and busybox awk; the
# BSD awk on macOS is untested.

BEGIN { # explicit character sets, the same in every awk and locale (no [A-Z] range)
  LETTERS = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_"
  DIGITS = "0123456789"
  # The characters a message prints of a label, a block path or a key the source chose (shown()).
  PRINTABLE = LETTERS DIGITS "-/%:. "
  # The functions that read files, render templates, or resolve paths, on the machine that runs tofu.
  split("file filebase64 filebase64sha256 filebase64sha512 fileexists fileset templatefile templatestring filemd5 filesha1 filesha256 filesha512 pathexpand abspath", ff, " ")
  for (i in ff) {
    fileFunc[ff[i]] = 1
  }
  # The one file function call a configuration may make, character for character: file() of the Caddyfile
  # in the configuration's own directory, which the site container runs (ADR 0045).
  CADDYFILE_READ = "(\"${path.module}/Caddyfile\")"
  # The site container's cost pins (ADR 0045, The container): each set once, as exactly this literal.
  npins = split("max_scale=1 cpu_limit=140 memory_limit_bytes=268435456", cp, " ")
  for (i = 1; i <= npins; i++) {
    split(cp[i], kv, "=")
    containerPinName[i] = kv[1]
    containerPin[kv[1]] = kv[2]
  }
  containerPinTag["max_scale"] = "container-max-scale"
  containerPinTag["cpu_limit"] = "container-cpu"
  containerPinTag["memory_limit_bytes"] = "container-memory"
  containerPinWhy["max_scale"] = "one instance at most, the worst case ADR 0045 prices at about EUR 2.12 a month"
  containerPinWhy["cpu_limit"] = "140 mvCPU, the size ADR 0045 prices"
  containerPinWhy["memory_limit_bytes"] = "0.25 GB, the size ADR 0045 prices"
  # The deprecated spellings beside the pinned ones: memory_limit (MB) beside memory_limit_bytes, and
  # http_option beside https_connections_only. Either could set what the pin does not see.
  ndeprecated = split("memory_limit http_option", containerDeprecatedName, " ")
  containerDeprecated["memory_limit"] = "memory_limit_bytes"
  containerDeprecated["http_option"] = "https_connections_only"
  # The settings a top-level terraform block may hold, each judged by its own rule. Anything else
  # (encryption, provider_meta, experiments, ...) changes how tofu runs on the admin machine.
  TERRAFORM_SETTINGS_TEXT = "required_version, required_providers, backend and cloud"
  split("required_version required_providers backend cloud", ts, " ")
  for (i in ts) {
    terraformSetting[ts[i]] = 1
  }
  # The top-level block types a configuration file, and a test file, may hold. Any other (moved, removed,
  # check, list, override_resource, a mock_provider in a configuration, ...) is a block no rule reads.
  CONFIG_BLOCKS_TEXT = "terraform, provider, resource, data, ephemeral, variable, locals, output, module and import"
  split("terraform provider resource data ephemeral variable locals output module import", cb, " ")
  for (i in cb) {
    configBlock[cb[i]] = 1
  }
  TEST_BLOCKS_TEXT = "run, mock_provider, variables and provider"
  split("run mock_provider variables provider", tb, " ")
  for (i in tb) {
    testBlock[tb[i]] = 1
  }
  # The blocks a test file's run and mock_provider may hold directly (issue #434): what tofu test reads
  # there, which shapes mocked values and never reaches Scaleway. tofu reads expect_failures only as an
  # attribute; its block spelling is allow-listed only because issue #434 lists it, and nothing should
  # rely on tofu accepting an expect_failures {} block. OpenTofu also reads override_resource and
  # override_data inside a mock_provider (they override that provider's resources and data sources), so
  # they are allowed there too. Only the direct children are judged: a block inside an allowed one (the
  # values of an override_resource) is that block's business.
  RUN_CHILDREN_TEXT = "override_resource, override_data, override_module, assert, variables, module, plan_options and expect_failures"
  split("override_resource override_data override_module assert variables module plan_options expect_failures", rc, " ")
  for (i in rc) {
    runChild[rc[i]] = 1
  }
  MOCK_CHILDREN_TEXT = "mock_resource, mock_data, override_resource and override_data"
  split("mock_resource mock_data override_resource override_data", mc, " ")
  for (i in mc) {
    mockChild[mc[i]] = 1
  }
  # What the provider "scaleway" block may set beyond the settings a rule of its own names (alias,
  # access_key, secret_key, profile, api_url): only what infra/site/providers.tf sets.
  PROVIDER_SETTINGS_TEXT = "organization_id and region"
  split("organization_id region alias access_key secret_key profile api_url", ps, " ")
  for (i in ps) {
    providerSetting[ps[i]] = 1
  }
  # The resource types the configuration may hold: exactly the ones infra/site uses (main.tf and the
  # front's container.tf). No data or ephemeral type is allowed. A new resource extends this list in the
  # same PR that adds it, so the review of that PR sees both. A type a specific rule names (api-key,
  # object, bucket-acl, iam-other, provider-type) is reported by that rule alone. The TLS stage left
  # the list with the pipeline's www domain (ADR 0045): only scaleway_edge_services_tls_stage "site",
  # which [tls-stage-lifecycle] judges, may stand, gated by pipeline_www for R8's rollback.
  split("scaleway_account_project scaleway_container scaleway_container_domain scaleway_container_namespace scaleway_edge_services_backend_stage scaleway_edge_services_cache_stage scaleway_edge_services_dns_stage scaleway_edge_services_head_stage scaleway_edge_services_pipeline scaleway_edge_services_plan scaleway_iam_application scaleway_iam_policy scaleway_object_bucket scaleway_object_bucket_policy scaleway_object_bucket_website_configuration", rt, " ")
  for (i in rt) {
    resourceType[rt[i]] = 1
  }
  # The settings the committed backend "s3" block may hold: none names a bucket, endpoint, role or secret.
  BACKEND_SETTINGS_TEXT = "key, use_lockfile, skip_credentials_validation, skip_region_validation, skip_requesting_account_id and skip_s3_checksum; the bucket, region and endpoint come from the git-ignored backend.hcl"
  split("key use_lockfile skip_credentials_validation skip_region_validation skip_requesting_account_id skip_s3_checksum", bs, " ")
  for (i in bs) {
    backendSetting[bs[i]] = 1
  }
  # Every file given is judged, an empty one too: FNR == 1 never fires for a zero-byte file.
  for (i = 1; i < ARGC; i++) {
    fileSeen[ARGV[i]] = 1
    isTest[ARGV[i]] = (ARGV[i] ~ /\.(tftest|tofutest)\.hcl$/)
  }
}

# An identifier just read: when it names a file function, the next character outside whitespace and
# comments decides whether it is called.
function ident(s) {
  if (s in fileFunc) {
    ffpend = s
  }
}

function problem(rule, file, line, msg) {
  print "[" rule "] " file (line == "" ? "" : ":" line) ": " msg
  problems++
  if (rule == "parse") {
    parsefailed++
  }
}

# A [parse] failure: the rest of the file is not read.
function unreadable(msg) {
  problem("parse", FILENAME, tokline, msg)
  broken = 1
}

# --- The parser: a stack of contexts. kind is "body" (a block's body, or the file), "obj" (an object
# constructor) or "expr" (inside parentheses or brackets, where only nesting is followed). name is the
# context's path element: "<type>[:<label>...]" for a block, the attribute's name for an object value,
# "{}" for an object inside an expression.

function path(   i, p) {
  p = ""
  for (i = 1; i <= sp; i++) {
    p = p (i > 1 ? "/" : "") name[i]
  }
  return p
}

function blockid(   i) { # the innermost block's instance number, 0 at the top level
  for (i = sp; i >= 1; i--) {
    if (kind[i] == "body") {
      return bid[i]
    }
  }
  return 0
}

# A block label as a path element: %, / and : are written %25, %2F and %3A, so a label never adds a
# segment or a label to the path, and the rules keep judging the block's real children (issue #434).
function escaped(s,   i, c, r) {
  r = ""
  for (i = 1; i <= length(s); i++) {
    c = substr(s, i, 1)
    if (c == "%") {
      c = "%25"
    } else if (c == "/") {
      c = "%2F"
    } else if (c == ":") {
      c = "%3A"
    }
    r = r c
  }
  return r
}

# A path element's label as the source writes it, the inverse of escaped(): a message that quotes a label
# names what the file holds, never the escape the rules judge it by.
function unescaped(s,   i, c, r) {
  r = ""
  for (i = 1; i <= length(s); i++) {
    c = substr(s, i, 3)
    if (c == "%25") {
      r = r "%"
    } else if (c == "%2F") {
      r = r "/"
    } else if (c == "%3A") {
      r = r ":"
    } else {
      r = r substr(s, i, 1)
      continue
    }
    i += 2
  }
  return r
}

# Text the source chose, as a message prints it: s when every character is in PRINTABLE or extra, what
# otherwise. A message reaches the CI log, where the legacy GitHub Actions annotation form (##[error] and
# the like) is matched anywhere in a line, so a label such as "##[error]x" must not be printed: it would
# forge an annotation. Every message that prints such text prints it through quoted() (a label),
# shownpath() (a block path, whose elements hold labels), shownkey() (a key, which may be quoted) or
# shownsource() (a source string); none of their character sets holds # or [.
function shown(s, extra, what,   i) {
  for (i = 1; i <= length(s); i++) {
    if (index(PRINTABLE extra, substr(s, i, 1)) == 0) {
      return what
    }
  }
  return s
}

# A label as a message quotes it: "<label>" as the source writes it, or an unprintable label.
function quoted(s) {
  return shown(s, "", "") == s ? "\"" s "\"" : "an unprintable label"
}

# A block path as a message prints it: its labels escaped, and the elements an object ({}) or an
# expression (( or [) adds.
function shownpath(p) {
  return shown(p, "{}[(", "a block path with an unprintable label")
}

# An attribute's or object's key as a message prints it.
function shownkey(n) {
  return shown(n, "", "an unprintable name")
}

# A source string as a message prints it, with what a registry, git or URL source holds besides.
function shownsource(s) {
  return shown(s, "<>@?=&+~", "an unprintable source")
}

# Whether s is an HCL identifier: a letter or _, then letters, digits, _ or -.
function isident(s,   i) {
  if (s == "" || index(LETTERS, substr(s, 1, 1)) == 0) {
    return 0
  }
  for (i = 2; i <= length(s); i++) {
    if (index(LETTERS DIGITS "-", substr(s, i, 1)) == 0) {
      return 0
    }
  }
  return 1
}

function push(k, n) {
  sp++
  kind[sp] = k
  name[sp] = n
  bid[sp] = 0
  if (k == "body") {
    nblk++
    bid[sp] = nblk
    bfile[nblk] = FILENAME
    bline[nblk] = tokline
    bpath[nblk] = path()
  }
  reset()
}

function pop() {
  if (sp == 0) {
    unreadable("a closing bracket has no opening one")
    return
  }
  sp--
  reset()
  skip = 1 # what follows a closed bracket on the same line is the rest of the statement
}

function reset() {
  bn = 0
  skip = 0
}

function attr(n, k, v) {
  nattr++
  afile[nattr] = FILENAME
  aline[nattr] = tokline
  apath[nattr] = path()
  ablk[nattr] = blockid()
  aname[nattr] = n
  akind[nattr] = k
  aval[nattr] = v
  if (rec && recattr == 0) { # the condition whose tokens are being kept (tok)
    recattr = nattr
  }
}

# One token: t is "I" (identifier), "S" (string; lit is 1 when it holds no template), "P" (one of
# { } ( ) [ ] , : =), "N" (a newline outside brackets that matter) or "X" (anything else).
function tok(t, v, lit, esc,   i, ok, n) {
  if (broken) {
    return
  }
  # A condition's value is kept as its tokens, joined by one space (a string quoted), until the newline,
  # comma or } that ends it at the depth it began: [container-min-scale-postcondition] reads it.
  recend = rec && sp == recsp && (t == "N" || (t == "P" && (v == "," || v == "}")))
  if (rec && !recend && t != "N") {
    rectext = rectext " " (t == "S" ? "\"" v "\"" : v)
  }
  if (pstate == 2) { # an attribute whose value began with a literal string: one string, or more?
    if (t == "N" || (t == "P" && (v == "," || v == "}"))) {
      attr(pa, "L", pval)
    } else {
      attr(pa, "E", "")
    }
    pstate = 0
  } else if (pstate == 3) { # an attribute whose value began with an identifier: that one, or more?
    if (t == "N" || (t == "P" && (v == "," || v == "}"))) {
      attr(pa, "I", pval)
    } else if (pval == "local" && t == "X" && v == ".") { # local.<name>, perhaps
      pstate = 5
      return
    } else {
      attr(pa, "E", "")
    }
    pstate = 0
  } else if (pstate == 5) { # local. read: a name must follow
    if (t == "I") {
      pstate = 6
      pval = v
      return
    }
    attr(pa, "E", "")
    pstate = 0
  } else if (pstate == 6) { # local.<name> read: that one reference, kept as kind "R" with the name, or more?
    if (t == "N" || (t == "P" && (v == "," || v == "}"))) {
      attr(pa, "R", pval)
    } else {
      attr(pa, "E", "")
    }
    pstate = 0
  } else if (pstate == 4) { # an attribute whose value began with a whole number: that one, or more?
    if (t == "N" || (t == "P" && (v == "," || v == "}"))) {
      attr(pa, "D", pval)
    } else {
      attr(pa, "E", "")
    }
    pstate = 0
  } else if (pstate == 1) { # the first token of an attribute's value
    pstate = 0
    if (t == "S" && lit) {
      pstate = 2
      pval = v
      return
    }
    if (t == "X" && v ~ /^[0-9]+$/) { # a lone whole number, such as 140, is kept as kind "D"
      pstate = 4
      pval = v
      return
    }
    if (t == "I") { # a lone identifier, such as true, is kept as kind "I"
      pstate = 3
      pval = v
      return
    }
    if (t == "P" && v == "{") {
      attr(pa, "O", "")
      push("obj", pa)
      return
    }
    attr(pa, "E", "")
  }
  if (recend) { # the condition's value has ended, and its attribute is recorded by now
    rec = 0
    if (recattr) {
      atext[recattr] = substr(rectext, 2)
    }
  }

  if (t == "P" && (v == "{" || v == "(" || v == "[")) {
    if (v == "{" && kind[sp] == "body" && !skip) { # a block header: <type> <label>... {
      if (bn < 1 || bt[1] != "I") {
        unreadable("a { that opens no block: a block starts with its type, an identifier")
        return
      }
      n = bv[1]
      for (i = 2; i <= bn; i++) {
        if (bt[i] == "S" && bl[i] && index(bv[i], "\\") == 0 && !be[i]) {
          if (sp == 0 && isTest[FILENAME] && (n == "run" || n == "mock_provider") && !isident(bv[i])) {
            nlab++
            labFile[nlab] = FILENAME
            labLine[nlab] = tokline
            labText[nlab] = bv[1] " " quoted(bv[i])
          }
          n = n ":" escaped(bv[i])
        } else if (bt[i] == "I") {
          n = n ":" bv[i]
        } else if (bt[i] == "S" && bl[i] && be[i]) {
          unreadable("a block label with a $${ or %%{ escape, which HCL decodes ($${ is ${): write the label without escapes")
          return
        } else if (bt[i] == "S" && bl[i]) {
          unreadable("a block label with a backslash, which HCL decodes (\\u005f is _): write the label without escapes")
          return
        } else {
          unreadable("a block label that is not a plain identifier or literal string (a template or another token)")
          return
        }
      }
      push("body", n)
      return
    }
    if (v == "{") {
      push("obj", "{}")
    } else {
      push("expr", v)
    }
    return
  }
  if (t == "P" && (v == "}" || v == ")" || v == "]")) {
    pop()
    return
  }
  if (kind[sp] == "expr") {
    return
  }
  if (t == "N" || (t == "P" && v == ",")) {
    reset()
    return
  }
  if (skip) {
    return
  }
  if (t == "P" && (v == "=" || v == ":") && bn == 1 && (bt[1] == "I" || (bt[1] == "S" && bl[1]))) {
    if (bt[1] == "S" && index(bv[1], "\\") > 0) {
      unreadable("a quoted key with a backslash, which HCL decodes (\\u005f is _): write the key without escapes")
      return
    }
    pa = bv[1]
    pstate = 1
    skip = 1
    if (pa == "condition") { # keep its tokens (above)
      rec = 1
      recsp = sp
      recattr = 0
      rectext = ""
    }
    return
  }
  bn++
  bt[bn] = t
  bv[bn] = v
  bl[bn] = lit
  be[bn] = esc
}

# --- The tokenizer, one line at a time; strings and templates may not span lines, block comments may.

FNR == 1 {
  endfile()
  sp = 0
  kind[0] = "body"
  name[0] = ""
  bid[0] = 0
  md = 0
  incomment = 0
  pstate = 0
  rec = 0
  broken = 0
  ffpend = ""
  reset()
  files++
  fileSeen[FILENAME] = 1
  isTest[FILENAME] = (FILENAME ~ /\.(tftest|tofutest)\.hcl$/)
}

{
  tokline = FNR
  line = $0
  n = length(line)
  for (i = 1; i <= n && !broken; i++) {
    c = substr(line, i, 1)
    nx = substr(line, i + 1, 1)
    if (incomment) {
      if (c == "*" && nx == "/") {
        incomment = 0
        i++
      }
      continue
    }
    if (md > 0 && mode[md] == "S") {
      if (c == "\\") {
        if (md == 1) {
          sval = sval substr(line, i, 2)
        }
        i++
        continue
      }
      if ((c == "$" || c == "%") && nx == c && substr(line, i + 2, 1) == "{") { # $${ and %%{ are literal
        if (md == 1) {
          sval = sval c "{"
          stresc = 1
        }
        i += 2
        continue
      }
      if ((c == "$" || c == "%") && nx == "{") {
        strlit = 0
        md++
        mode[md] = "T"
        depth[md] = 0
        i++
        continue
      }
      if (c == "\"") {
        md--
        if (md == 0) {
          tok("S", sval, strlit, stresc)
        }
        continue
      }
      if (md == 1) {
        sval = sval c
      }
      continue
    }
    if (c == "#" || (c == "/" && nx == "/")) {
      break
    }
    if (c == "/" && nx == "*") {
      incomment = 1
      i++
      continue
    }
    if (ffpend != "" && c != " " && c != "\t" && c != "\r") { # a file function's name: is it called?
      # The one read allowed: the site container's Caddyfile, a committed file beside the configuration
      # that reads it (ADR 0045), named exactly so, in a configuration file. A test file, another function,
      # another path or an argument built any other way is still a call.
      if (c == "(" && !(ffpend == "file" && !isTest[FILENAME] && substr(line, i, length(CADDYFILE_READ)) == CADDYFILE_READ)) {
        nff++
        ffFile[nff] = FILENAME
        ffLine[nff] = FNR
        ffName[nff] = ffpend
      }
      ffpend = ""
    }
    if (c == "\"") {
      md++
      mode[md] = "S"
      if (md == 1) {
        sval = ""
        strlit = 1
        stresc = 0
      }
      continue
    }
    if (md > 0) { # inside a template's ${...}: follow its braces, emit nothing
      if (index(LETTERS, c) > 0) { # an identifier, read only for a file function's call
        j = i
        while (j < n && index(LETTERS DIGITS "-", substr(line, j + 1, 1)) > 0) {
          j++
        }
        ident(substr(line, i, j - i + 1))
        i = j
      } else if (c == "{") {
        depth[md]++
      } else if (c == "}") {
        if (depth[md] == 0) {
          md--
        } else {
          depth[md]--
        }
      }
      continue
    }
    if (c == "<" && nx == "<") {
      problem("parse", FILENAME, FNR, "a heredoc or <<, which these rules do not read: use a quoted string or jsonencode()")
      broken = 1
      continue
    }
    if (index(LETTERS, c) > 0) {
      j = i
      while (j < n && index(LETTERS DIGITS "-", substr(line, j + 1, 1)) > 0) {
        j++
      }
      tok("I", substr(line, i, j - i + 1), 0)
      ident(substr(line, i, j - i + 1))
      i = j
      continue
    }
    if (index(DIGITS, c) > 0) {
      j = i
      while (j < n && index(DIGITS ".eE", substr(line, j + 1, 1)) > 0) {
        j++
      }
      tok("X", substr(line, i, j - i + 1), 0)
      i = j
      continue
    }
    if (c == " " || c == "\t" || c == "\r") {
      continue
    }
    if (c == "=") {
      if (nx == "=" || nx == ">") {
        tok("X", c nx, 0)
        i++
      } else {
        tok("P", "=", 0)
      }
      continue
    }
    if (c == "!" || c == "<" || c == ">") {
      if (nx == "=") {
        i++
      }
      tok("X", c, 0)
      continue
    }
    if (c == "{" || c == "}" || c == "(" || c == ")" || c == "[" || c == "]" || c == "," || c == ":") {
      tok("P", c, 0)
      continue
    }
    tok("X", c, 0)
  }
  if (md > 0 && !broken) {
    problem("parse", FILENAME, FNR, "a string or template does not end on its line")
    broken = 1
  }
  tok("N", "", 0)
}

# The end of the last file read. Every line ended with a newline token, so no attribute is pending; what
# is left to check is that nothing is still open. FILENAME is already the next file's here, so the
# problem names lastfile.
function endfile() {
  if (files == 0) {
    return
  }
  if (!broken) {
    if (incomment) {
      problem("parse", lastfile, lastline, "a /* comment does not end")
    } else if (sp != 0) {
      problem("parse", lastfile, lastline, "a block, object or bracket is not closed")
    }
  }
}

{
  lastfile = FILENAME
  lastline = FNR
}

function dirof(f,   d) {
  d = f
  sub("/[^/]*$", "", d)
  return d
}

# An applied root: a directory directly under the root argument (today <root>/site). The maintainer runs
# init and apply there, so it is never a module and always needs its own backend and required_version.
function isroot(d) {
  return d != root && dirof(d) == root
}

# <dir>/<a local ./ source>, without ./ segments or a trailing /.
function joinpath(dir, s,   d) {
  d = dir "/" substr(s, 3)
  while (sub("/[.]/", "/", d)) {
  }
  while (sub("//", "/", d)) {
  }
  sub("/[.]$", "", d)
  sub("/$", "", d)
  return d
}

END {
  endfile()
  if (parsefailed) {
    exit
  }

  # Calls found by the tokenizer, reported only now, so a [parse] failure elsewhere keeps them unprinted.
  for (i = 1; i <= nff; i++) {
    problem("file-function", ffFile[i], ffLine[i], ffName[i] "(): it reads the machine that runs tofu, so the plan would differ per machine and could carry a local file into the state")
  }

  # A run or mock_provider label that is no identifier: tofu test names runs and mocks by it.
  for (i = 1; i <= nlab; i++) {
    problem("test-label", labFile[i], labLine[i], labText[i] ": a run or mock_provider label must be an identifier (a letter or _, then letters, digits, _ or -)")
  }

  # Blocks.
  for (b = 1; b <= nblk; b++) {
    f = bfile[b]
    p = bpath[b]
    m = split(p, el, "/")
    last = el[m]
    k = split(last, lab, ":")
    typ = lab[1]
    # At any depth, in a resource, a removed block or a test file alike.
    if (typ == "provisioner" || typ == "connection") {
      problem("provisioner", f, bline[b], "a " typ " block (" shownpath(p) "): a provisioner runs commands, or reaches another machine, on whichever machine applies or tests, where the Scaleway keys and the GitHub admin token live")
    }
    if (m == 1 && !isTest[f] && typ == "import") {
      problem("import-block", f, bline[b], "an import block: the Runbook creates every resource fresh, so an import would adopt whatever object its id names into the state unasked (ADR 0039)")
    }
    if (m == 1 && isTest[f] && !(typ in testBlock)) {
      problem("block-type", f, bline[b], "a top-level " typ " block in a test file: a test file holds only " TEST_BLOCKS_TEXT ", since a block no rule knows (override_resource, override_data, ...) is a block no rule reads")
    }
    if (m == 1 && !isTest[f] && !(typ in configBlock)) {
      problem("block-type", f, bline[b], "a top-level " typ " block in a configuration file: it holds only " CONFIG_BLOCKS_TEXT ", since a block no rule knows (moved, removed, check, a mock_provider, ...) is a block no rule reads")
    }
    if (isTest[f]) {
      if (m == 1 && typ == "mock_provider") {
        mockBlock[b] = 1
        if (k == 2 && lab[2] == "scaleway") {
          mock[b] = f
        }
      }
      if (m == 2 && el[1] ~ "^run(:|$)" && !(typ in runChild)) {
        problem("test-nested-block", f, bline[b], "a " typ " block directly in " shownpath(el[1]) ": a run block holds only " RUN_CHILDREN_TEXT ", since a block no rule knows is a block no rule reads")
      }
      if (m == 2 && el[1] ~ "^mock_provider(:|$)" && !(typ in mockChild)) {
        problem("test-nested-block", f, bline[b], "a " typ " block directly in " shownpath(el[1]) ": a mock_provider holds only " MOCK_CHILDREN_TEXT ", since a block no rule knows is a block no rule reads")
      }
      if (m == 1 && typ == "provider") {
        problem("test-provider", f, bline[b], "a test file configures a real provider (" last "): it must only mock_provider \"scaleway\", so tofu test never reaches Scaleway")
      }
      if (m == 2 && el[1] ~ "^run:" && typ == "module") {
        nmod++
        modBlock[nmod] = b
        isModule[b] = 1
      }
      continue
    }
    configDir[dirof(f)] = 1
    if (m == 1 && typ == "resource" && lab[2] == "scaleway_container") {
      containerBlock[b] = 1 # its cost pins are judged after the attributes, below
      containers++          # more than one is [container-repeat], judged at the end
    }
    if (typ == "resource" || typ == "data" || typ == "ephemeral") {
      if (lab[2] !~ /^scaleway_/) {
        problem("provider-type", f, bline[b], typ " " quoted(unescaped(lab[2])) " belongs to no scaleway provider: infra/ uses scaleway/scaleway alone")
      }
      if (lab[2] == "scaleway_iam_api_key") {
        problem("api-key", f, bline[b], "a " typ " scaleway_iam_api_key: the deploy API key is created by hand, so no secret reaches the state")
      } else if (typ == "resource" && lab[2] == "scaleway_object") {
        objects[b] = 1 # reported after the attributes, below
      } else if (m == 1 && typ == "resource" && k == 3 && lab[2] == "scaleway_edge_services_tls_stage" && lab[3] == "site") {
        tlsStage[b] = dirof(f) "/" p "/lifecycle" # judged after the attributes, below; keyed by directory, since every root may hold its own stage
      } else if (typ == "resource" && lab[2] == "scaleway_object_bucket_acl") {
        problem("bucket-acl", f, bline[b], "a scaleway_object_bucket_acl: the bucket policy alone decides who reads the site (ADR 0039), and an ACL could make every object, or the bucket, public")
      } else if (typ == "resource" && lab[2] == "scaleway_iam_policy") {
        iamPolicies++
      } else if (typ == "resource" && lab[2] == "scaleway_iam_application") {
        iamApplications++
      } else if (typ == "resource" && lab[2] ~ /^scaleway_iam_/) {
        problem("iam-other", f, bline[b], "a resource " quoted(unescaped(lab[2])) ": infra/ creates exactly one IAM application and its one policy, nothing else in IAM")
      } else if (lab[2] ~ /^scaleway_/ && !(typ == "resource" && (lab[2] in resourceType))) {
        problem("resource-type", f, bline[b], typ " " quoted(unescaped(lab[2])) " is no resource type infra/site uses (and no data or ephemeral type is allowed): a new type extends the allow list in dev/infra-rules.awk in the same PR, so its review sees both")
      }
    }
    if (m == 2 && el[1] == "terraform" && !(typ in terraformSetting)) {
      problem("terraform-setting", f, bline[b], "the terraform block holds " typ ": it may hold only " TERRAFORM_SETTINGS_TEXT ", since anything else (an encryption key provider, provider_meta, experiments) changes how tofu runs on the admin machine")
    }
    if (m == 2 && el[1] == "provider:scaleway") {
      problem("provider-setting", f, bline[b], "the provider \"scaleway\" block holds a " typ " block: it may set only " PROVIDER_SETTINGS_TEXT ", since any other setting changes what the plan reaches")
    }
    if (m == 1 && typ == "provider" && lab[2] != "scaleway") {
      problem("provider-type", f, bline[b], "a provider " quoted(unescaped(lab[2])) ": infra/ uses scaleway/scaleway alone")
    }
    if (m == 1 && typ == "module") {
      nmod++
      modBlock[nmod] = b
      isModule[b] = 1
    }
    if (m == 2 && el[1] == "terraform" && typ == "backend") {
      backends[dirof(f)]++
      if (lab[2] != "s3") {
        problem("backend-s3", f, bline[b], "backend " quoted(unescaped(lab[2])) ": the state lives in a Scaleway bucket through the s3 backend, never local")
      }
    }
    if (m == 3 && el[1] == "terraform" && el[2] == "backend:s3") {
      problem("backend-setting", f, bline[b], "a " last " block in the committed backend \"s3\" block (" shownpath(p) "): it may set only " BACKEND_SETTINGS_TEXT)
    }
    if (m == 2 && el[1] == "terraform" && typ == "cloud") {
      problem("cloud-block", f, bline[b], "a cloud block: the state lives in a Scaleway bucket through the s3 backend")
    }
  }

  # Attributes.
  for (a = 1; a <= nattr; a++) {
    f = afile[a]
    p = apath[a]
    n = aname[a]
    if (isTest[f]) {
      if (n == "alias" && (ablk[a] in mock) && p == bpath[ablk[a]]) {
        aliased[ablk[a]] = 1
      }
      if (n == "source" && (ablk[a] in mockBlock) && p == bpath[ablk[a]]) {
        problem("mock-source", f, aline[a], "source in " shownpath(p) ": the mocks would come from files outside the test file, so what it mocks is not what it holds")
      }
      if (n == "providers" && p ~ "^run:[^/]+$") {
        problem("test-providers", f, aline[a], "providers in a run block (" shownpath(p) "): it hands the run a provider configuration other than the file's unaliased mock_provider \"scaleway\"")
      }
      if (n == "source" && (ablk[a] in isModule) && p == bpath[ablk[a]]) {
        modSource[ablk[a]] = (akind[a] == "L") ? aval[a] : "<an expression>"
      }
      continue
    }
    if (p ~ "^provider:[^/]+$") {
      if (n == "alias") {
        problem("provider-alias", f, aline[a], "alias in " shownpath(p) ": a second provider configuration under another name, which resources and tests could reach unseen")
      }
      if (n == "access_key" || n == "secret_key" || n == "profile") {
        problem("provider-credentials", f, aline[a], n " in " shownpath(p) ": credentials come from SCW_ACCESS_KEY and SCW_SECRET_KEY on the admin machine, never from a file git holds")
      }
      if (n == "api_url") {
        problem("provider-endpoint", f, aline[a], n " in " shownpath(p) ": the provider talks to Scaleway's own API; an endpoint in a file git holds could send the admin machine's credentials elsewhere")
      }
      if (p == "provider:scaleway" && !(n in providerSetting)) {
        problem("provider-setting", f, aline[a], shownkey(n) " in " shownpath(p) ": it may set only " PROVIDER_SETTINGS_TEXT ", since any other setting (a default project_id, ...) changes what the plan reaches")
      }
    }
    if (n == "acl") {
      problem("acl", f, aline[a], "an acl argument, even \"private\": the bucket policy alone decides who reads the site (ADR 0039)")
    }
    if (n == "visibility") {
      problem("visibility", f, aline[a], "a visibility argument, even \"private\": an object's visibility is an ACL, and the bucket policy alone decides who reads the site (ADR 0039)")
    }
    if (p == "terraform/backend:s3" && !(n in backendSetting)) {
      problem("backend-setting", f, aline[a], shownkey(n) " in the committed backend \"s3\" block: it may set only " BACKEND_SETTINGS_TEXT)
    }
    if (p == "terraform" && !(n in terraformSetting)) {
      problem("terraform-setting", f, aline[a], shownkey(n) " in a terraform block: it may hold only " TERRAFORM_SETTINGS_TEXT ", since anything else changes how tofu runs on the admin machine")
    }
    if (p == "terraform" && n == "required_version") {
      versions[dirof(f)]++
      if (akind[a] != "L" || aval[a] != pinned) {
        problem("required-version", f, aline[a], "required_version is not \"" pinned "\", infra/tofu.yaml's tofuVersion without its v: they must name the one release dev/tofu.sh runs")
      }
    }
    if (p == "terraform/required_providers") {
      if (akind[a] != "O") {
        problem("provider-source", f, aline[a], "required_providers entry " shownkey(n) " is not an object with source = \"scaleway/scaleway\"")
      } else {
        nprov++
        provPath[nprov] = p "/" n
        provName[nprov] = n
        provAt[nprov] = f ":" aline[a]
      }
    }
    if (p ~ "^terraform/required_providers/[^/]+$" && n == "source") {
      provSource[p] = (akind[a] == "L") ? aval[a] : "<an expression>"
    }
    if (n == "source" && (ablk[a] in isModule) && p == bpath[ablk[a]]) {
      modSource[ablk[a]] = (akind[a] == "L") ? aval[a] : "<an expression>"
    }
    if (n == "create_before_destroy" && akind[a] == "I" && aval[a] == "true" && p == bpath[ablk[a]]) {
      createFirst[dirof(f) "/" p] = 1
    }
    # The site container's own attributes (not those of its nested blocks), for its cost pins.
    if ((ablk[a] in containerBlock) && p == bpath[ablk[a]]) {
      ck = ablk[a] SUBSEP n
      containerSet[ck]++
      containerKind[ck] = akind[a]
      containerVal[ck] = aval[a]
      containerAt[ck] = aline[a]
    }
    # A configuration's locals, by directory (a module's scope), for the min_scale pin that may read one.
    if (p == "locals") {
      lk = dirof(f) SUBSEP n
      localSet[lk]++
      localKind[lk] = akind[a]
      localVal[lk] = aval[a]
    }
    # Every postcondition's condition, for the one that checks the container's min_scale.
    if (n == "condition" && p ~ "/lifecycle/postcondition$") {
      nconds++
      condAttr[nconds] = a
    }
    if (p ~ "^resource:scaleway_iam_(policy|application):[^/]+$" && (n == "count" || n == "for_each")) {
      problem("iam-repeat", f, aline[a], n " on " shownpath(p) ": infra/ creates exactly one IAM application and one policy")
    }
    # The front is one container in one namespace (ADR 0045): a repeat multiplies the cost pins' ceiling.
    if (p ~ "^resource:scaleway_container(_namespace)?:[^/]+$" && (n == "count" || n == "for_each")) {
      problem("container-repeat", f, aline[a], n " on " shownpath(p) ": infra/ runs exactly one front container in one namespace, and a repeat multiplies the cost pins' ceiling (ADR 0045)")
    }
  }

  for (b = 1; b <= nblk; b++) {
    if (b in objects) {
      problem("object", bfile[b], bline[b], "a scaleway_object resource: tofu never uploads an object, the release deploys them (ADR 0039)")
    }
    # The TLS stage's managed certificate is requested only at its creation, so R8 may replace it, and
    # Scaleway refuses to delete a TLS stage the DNS stage references (issue #541).
    if ((b in tlsStage) && !(tlsStage[b] in createFirst)) {
      problem("tls-stage-lifecycle", bfile[b], bline[b], "resource scaleway_edge_services_tls_stage \"site\" has no lifecycle block with create_before_destroy = true: a replacement would delete the stage first, which Scaleway refuses while the DNS stage references it")
    }
    # The site container's cost pins (ADR 0045, The container): one instance of 140 mvCPU and 0.25 GB at
    # most, each a literal in the file, never a provider default or a value a tfvars file could raise; no
    # deprecated spelling beside them; and at most one instance kept warm.
    if (b in containerBlock) {
      shownres = shownpath(bpath[b])
      for (j = 1; j <= npins; j++) {
        n = containerPinName[j]
        ck = b SUBSEP n
        if (!(ck in containerSet)) {
          problem(containerPinTag[n], bfile[b], bline[b], shownres " sets no " n ": it must set " n " = " containerPin[n] " (" containerPinWhy[n] "), not leave it to the provider's default")
        } else if (containerSet[ck] != 1 || containerKind[ck] != "D" || containerVal[ck] != containerPin[n]) {
          problem(containerPinTag[n], bfile[b], containerAt[ck], shownres " sets " n " to something other than the literal " containerPin[n] " (" containerPinWhy[n] "): another value, or an expression a tfvars file could set, is a cost nobody decided")
        }
      }
      for (j = 1; j <= ndeprecated; j++) {
        n = containerDeprecatedName[j]
        ck = b SUBSEP n
        if (ck in containerSet) {
          problem("container-deprecated-attribute", bfile[b], containerAt[ck], shownres " sets " n ", the deprecated spelling of " containerDeprecated[n] ": the pins judge " containerDeprecated[n] " alone, so " n " could set what they do not see")
        }
      }
      # min_scale may also read a local set once, in the container's own directory, to the literal 0 or 1:
      # the value is then written once and read by the pin and its postcondition alike.
      ck = b SUBSEP "min_scale"
      msexpr = "" # the expression an accepted pin sets, as tokens: its literal, or local . <name>
      if (!(ck in containerSet)) {
        problem("container-min-scale", bfile[b], bline[b], shownres " sets no min_scale: it must set min_scale = 0 or 1, the instances ADR 0045 allows to be kept warm")
      } else if (containerSet[ck] == 1 && containerKind[ck] == "D" && (containerVal[ck] == "0" || containerVal[ck] == "1")) {
        msexpr = containerVal[ck]
      } else if (containerSet[ck] == 1 && containerKind[ck] == "R") {
        lk = dirof(bfile[b]) SUBSEP containerVal[ck]
        if (localSet[lk] == 1 && localKind[lk] == "D" && (localVal[lk] == "0" || localVal[lk] == "1")) {
          msexpr = "local . " containerVal[ck]
        } else {
          problem("container-min-scale", bfile[b], containerAt[ck], shownres " sets min_scale = local." shownkey(containerVal[ck]) ", which is no local set once to the literal 0 or 1: more instances kept warm are a cost nobody decided")
        }
      } else {
        problem("container-min-scale", bfile[b], containerAt[ck], shownres " sets min_scale to something other than the literal 0 or 1, or a local set to one: more instances kept warm are a cost nobody decided")
      }
      # The check after an apply must compare the platform's min_scale with what the pin sets, so the two
      # cannot drift: a hard-coded 0 beside a local would still pass once the local is raised.
      if (msexpr != "") {
        for (j = 1; j <= nconds; j++) {
          a = condAttr[j]
          if (afile[a] != bfile[b] || apath[a] != bpath[b] "/lifecycle/postcondition") {
            continue
          }
          ct = atext[a]
          if (index(" " ct " ", " self . min_scale ") == 0) {
            continue
          }
          if (ct != "self . min_scale == " msexpr && ct != msexpr " == self . min_scale") {
            msshown = msexpr
            sub(/ [.] /, ".", msshown)
            problem("container-min-scale-postcondition", afile[a], aline[a], shownres " has a postcondition comparing self.min_scale with something other than what min_scale sets (" shownkey(msshown) "): the condition must be self.min_scale == the same expression, so the check after an apply follows the pin")
          }
        }
      }
    }
  }

  for (i = 1; i <= nprov; i++) {
    s = (provPath[i] in provSource) ? provSource[provPath[i]] : "<none>"
    if (s != "scaleway/scaleway") {
      split(provAt[i], at, ":")
      problem("provider-source", at[1], at[2], "required_providers entry " shownkey(provName[i]) " has source " shownsource(s) ", not \"scaleway/scaleway\": infra/ uses that provider alone")
    }
  }

  for (i = 1; i <= nmod; i++) {
    b = modBlock[i]
    f = bfile[b]
    s = (b in modSource) ? modSource[b] : "<none>"
    if (s !~ "^[.]/") {
      problem("module-source", f, bline[b], shownpath(bpath[b]) " has source " shownsource(s) ": a module may only be a local ./ path in this repository, never a registry, git or URL source")
    } else if (s ~ "(^|/)[.][.](/|$)" || index(s, "\\") > 0) {
      problem("module-source", f, bline[b], shownpath(bpath[b]) " has source " shownsource(s) ": a .. segment (or an escape) can lead out of the configuration, to files these rules do not read")
    } else {
      # A segment starting with . other than . itself names a hidden directory, such as .terraform, which
      # the rules prune: tofu would load a module there that no rule reads. The directory is still named
      # as a module below, so the refusal is this one line, not a [backend-count] beside it.
      if (s ~ "(^|/)[.][^/]") {
        problem("module-source", f, bline[b], shownpath(bpath[b]) " has source " shownsource(s) ": a segment starting with . names a hidden directory (such as .terraform, which these rules prune), so tofu would load a module no rule reads")
      }
      if (!isTest[f]) {
        # A local module's directory is no root configuration: it needs no backend or required_version.
        # Only a configuration's module grants that: a test file's run-block module may name a root (a test
        # of it), but tofu test reads the file only where infra-check runs it, so it exempts nothing. A
        # module never names an applied root, which must keep its own backend and required_version.
        base = dirof(f)
        d = joinpath(base, s)
        if (isroot(d)) {
          problem("module-source", f, bline[b], shownpath(bpath[b]) " has source " shownsource(s) ", which is the applied root " d ": a module may not name a root, whose backend and required_version must hold whatever reaches it")
        } else if (d != base) {
          moduleDir[d] = 1
        }
      }
    }
  }

  # A file directly in the root's directory is no applied root's: the root holds only root directories.
  for (f in fileSeen) {
    if (dirof(f) == root) {
      problem("root-file", f, "", "a configuration or test file directly under " root ": every configuration lives in a root directory beneath it (such as " root "/site), which is applied, holds its own backend and is never a module")
    }
  }

  if (iamPolicies != 1) {
    problem("iam-policy-count", root, "", iamPolicies + 0 " scaleway_iam_policy resources, want exactly one: the deploy application's")
  }
  # The count speaks even when a second container's pins are refused too: one refusal never hides another.
  if (containers > 1) {
    problem("container-repeat", root, "", containers " scaleway_container resources, want at most one: the site's front (ADR 0045), whose cost pins bound one container's instances")
  }
  if (iamApplications != 1) {
    problem("iam-application-count", root, "", iamApplications + 0 " scaleway_iam_application resources, want exactly one: the deploy application")
  }

  # The root's own directory is never applied: [root-file] already refuses every file in it.
  for (d in configDir) {
    if (d == root || ((d in moduleDir) && !isroot(d))) {
      continue
    }
    if (backends[d] + 0 != 1) {
      problem("backend-count", d, "", backends[d] + 0 " backends, want exactly one: backend \"s3\"")
    }
    if (versions[d] + 0 != 1) {
      problem("required-version", d, "", versions[d] + 0 " required_version settings, want exactly one, \"" pinned "\"")
    }
  }

  for (b in mock) {
    if (!(b in aliased)) {
      mocked[mock[b]] = 1
    }
  }
  for (f in isTest) {
    if (isTest[f] && !(f in mocked)) {
      problem("test-mock", f, 1, "no mock_provider \"scaleway\" without an alias: tofu test would run the real provider")
    }
  }
}
