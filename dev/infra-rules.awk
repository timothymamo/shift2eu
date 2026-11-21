# The static rules of the OpenTofu configuration under an infra root, for dev/infra-rules.sh (issue #422).
# It reads every configuration file (*.tf, *.tofu) and test file (*.tftest.hcl, *.tofutest.hcl) given,
# tokenizes each as HCL, and prints one line per broken rule:
#   [<rule>] <file>:<line>: <what is wrong and why>
# and nothing when every rule holds. dev/infra-rules.sh documents the rules; the tester's fixtures in
# dev/testdata/infra-invalid match on the [<rule>] tag.
#
# The tokenizer knows HCL's strings (with ${...} and %{...} templates, which may nest strings), comments
# (#, // and /* */), identifiers, numbers and punctuation. The parser above it tracks only what the rules
# need: blocks with their type and labels, and attributes with their name and whether their value is one
# literal string ("L", its value kept), an object ("O") or any other expression ("E"). Anything it does
# not read, a heredoc or an unbalanced string, brace or comment, is a [parse] failure: the rules fail
# closed rather than pass a file they could not read.
#
# Usage: awk -v pinned=<required_version> -v root=<infra root> -f dev/infra-rules.awk <files...>
# POSIX awk only (mawk in the VM, the BSD awk on macOS, gawk in CI).

function problem(rule, file, line, msg) {
  print "[" rule "] " file (line == "" ? "" : ":" line) ": " msg
  problems++
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
    problem("parse", FILENAME, tokline, "a closing bracket has no opening one")
    broken = 1
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
}

# One token: t is "I" (identifier), "S" (string; lit is 1 when it holds no template), "P" (one of
# { } ( ) [ ] , : =), "N" (a newline outside brackets that matter) or "X" (anything else).
function tok(t, v, lit,   i, ok, n) {
  if (broken) {
    return
  }
  if (pstate == 2) { # an attribute whose value began with a literal string: one string, or more?
    if (t == "N" || (t == "P" && (v == "," || v == "}"))) {
      attr(pa, "L", pval)
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
    if (t == "P" && v == "{") {
      attr(pa, "O", "")
      push("obj", pa)
      return
    }
    attr(pa, "E", "")
  }

  if (t == "P" && (v == "{" || v == "(" || v == "[")) {
    if (v == "{" && kind[sp] == "body" && !skip && bn >= 1 && bt[1] == "I") {
      ok = 1
      n = bv[1]
      for (i = 2; i <= bn; i++) {
        if (bt[i] == "S" && bl[i]) {
          n = n ":" bv[i]
        } else if (bt[i] == "I") {
          n = n ":" bv[i]
        } else {
          ok = 0
        }
      }
      if (ok) {
        push("body", n)
        return
      }
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
    pa = bv[1]
    pstate = 1
    skip = 1
    return
  }
  bn++
  bt[bn] = t
  bv[bn] = v
  bl[bn] = lit
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
  broken = 0
  reset()
  files++
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
          tok("S", sval, strlit)
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
    if (c == "\"") {
      md++
      mode[md] = "S"
      if (md == 1) {
        sval = ""
        strlit = 1
      }
      continue
    }
    if (md > 0) { # inside a template's ${...}: follow its braces, emit nothing
      if (c == "{") {
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
    if (c ~ /[A-Za-z_]/) {
      j = i
      while (j < n && substr(line, j + 1, 1) ~ /[A-Za-z0-9_-]/) {
        j++
      }
      tok("I", substr(line, i, j - i + 1), 0)
      i = j
      continue
    }
    if (c ~ /[0-9]/) {
      j = i
      while (j < n && substr(line, j + 1, 1) ~ /[0-9.eE]/) {
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
    if (c ~ /[][{}(),:]/) {
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
  sub(/\/[^\/]*$/, "", d)
  return d
}

END {
  endfile()

  # Blocks.
  for (b = 1; b <= nblk; b++) {
    f = bfile[b]
    p = bpath[b]
    m = split(p, el, "/")
    last = el[m]
    k = split(last, lab, ":")
    typ = lab[1]
    if (isTest[f]) {
      if (m == 1 && typ == "mock_provider" && k == 2 && lab[2] == "scaleway") {
        mock[b] = f
      }
      if (m == 1 && typ == "provider") {
        problem("test-provider", f, bline[b], "a test file configures a real provider (" last "): it must only mock_provider \"scaleway\", so tofu test never reaches Scaleway")
      }
      continue
    }
    configDir[dirof(f)] = 1
    if (typ == "resource" || typ == "data" || typ == "ephemeral" || typ == "list") {
      if (lab[2] !~ /^scaleway_/) {
        problem("provider-type", f, bline[b], typ " \"" lab[2] "\" belongs to no scaleway provider: infra/ uses scaleway/scaleway alone")
      }
      if (lab[2] == "scaleway_iam_api_key") {
        problem("api-key", f, bline[b], "a " typ " scaleway_iam_api_key: the deploy API key is created by hand, so no secret reaches the state")
      } else if (typ == "resource" && lab[2] == "scaleway_object_bucket_acl") {
        problem("bucket-acl", f, bline[b], "a scaleway_object_bucket_acl: the bucket policy alone decides who reads the site (ADR 0039), and an ACL could make every object, or the bucket, public")
      } else if (typ == "resource" && lab[2] == "scaleway_iam_policy") {
        iamPolicies++
      } else if (typ == "resource" && lab[2] == "scaleway_iam_application") {
        iamApplications++
      } else if (typ == "resource" && lab[2] ~ /^scaleway_iam_/) {
        problem("iam-other", f, bline[b], "a resource " lab[2] ": infra/ creates exactly one IAM application and its one policy, nothing else in IAM")
      }
    }
    if (m == 1 && typ == "provider" && lab[2] != "scaleway") {
      problem("provider-type", f, bline[b], "a provider \"" lab[2] "\": infra/ uses scaleway/scaleway alone")
    }
    if (m == 1 && typ == "module") {
      nmod++
      modBlock[nmod] = b
    }
    if (m == 2 && el[1] == "terraform" && typ == "backend") {
      backends[dirof(f)]++
      if (lab[2] != "s3") {
        problem("backend-s3", f, bline[b], "backend \"" lab[2] "\": the state lives in a Scaleway bucket through the s3 backend, never local")
      }
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
      continue
    }
    if (n == "acl") {
      problem("acl", f, aline[a], "an acl argument, even \"private\": the bucket policy alone decides who reads the site (ADR 0039)")
    }
    if (n == "visibility") {
      problem("visibility", f, aline[a], "a visibility argument, even \"private\": an object's visibility is an ACL, and the bucket policy alone decides who reads the site (ADR 0039)")
    }
    if (p == "terraform" && n == "required_version") {
      versions[dirof(f)]++
      if (akind[a] != "L" || aval[a] != pinned) {
        problem("required-version", f, aline[a], "required_version is not \"" pinned "\", infra/tofu.yaml's tofuVersion without its v: they must name the one release dev/tofu.sh runs")
      }
    }
    if (p == "terraform/required_providers") {
      if (akind[a] != "O") {
        problem("provider-source", f, aline[a], "required_providers entry " n " is not an object with source = \"scaleway/scaleway\"")
      } else {
        nprov++
        provPath[nprov] = p "/" n
        provName[nprov] = n
        provAt[nprov] = f ":" aline[a]
      }
    }
    if (p ~ /^terraform\/required_providers\/[^\/]+$/ && n == "source") {
      provSource[p] = (akind[a] == "L") ? aval[a] : "<an expression>"
    }
    if (p ~ /^module:[^\/]+$/ && n == "source") {
      modSource[p] = (akind[a] == "L") ? aval[a] : "<an expression>"
    }
    if (p ~ /^resource:scaleway_iam_(policy|application):[^\/]+$/ && (n == "count" || n == "for_each")) {
      problem("iam-repeat", f, aline[a], n " on " p ": infra/ creates exactly one IAM application and one policy")
    }
  }

  for (i = 1; i <= nprov; i++) {
    s = (provPath[i] in provSource) ? provSource[provPath[i]] : "<none>"
    if (s != "scaleway/scaleway") {
      split(provAt[i], at, ":")
      problem("provider-source", at[1], at[2], "required_providers entry " provName[i] " has source " s ", not \"scaleway/scaleway\": infra/ uses that provider alone")
    }
  }

  for (i = 1; i <= nmod; i++) {
    b = modBlock[i]
    s = (bpath[b] in modSource) ? modSource[bpath[b]] : "<none>"
    if (s !~ /^\.\//) {
      problem("module-source", bfile[b], bline[b], bpath[b] " has source " s ": a module may only be a local ./ path in this repository, never a registry, git or URL source")
    }
  }

  if (iamPolicies != 1) {
    problem("iam-policy-count", root, "", iamPolicies + 0 " scaleway_iam_policy resources, want exactly one: the deploy application's")
  }
  if (iamApplications != 1) {
    problem("iam-application-count", root, "", iamApplications + 0 " scaleway_iam_application resources, want exactly one: the deploy application")
  }

  for (d in configDir) {
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
