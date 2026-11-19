// shift2eu's page script (PRD #297). Every page loads it. It sends nothing:
// test/site scans it for every browser API that can send data, load a
// resource or reach another window, and refuses any computed property name.
// The framing guard, the download function, the unregister function and the
// report's insertion point are the only exceptions, each in its own function.
"use strict";

// The one Trusted Types policy, which the Content Security Policy names. The
// report is the HTML reporter's fragment, escaped in Go: the policy passes it
// through, so the one insertion point is the only place markup enters the page.
// A browser without Trusted Types has no trustedTypes, and inserts the string.
const reportPolicy = typeof trustedTypes === "undefined" ? null : trustedTypes.createPolicy("shift2eu-report", {
  createHTML: function (html) {
    return html;
  },
});

// The no-WebAssembly message, the same as the page's noscript message.
const noWebAssembly = "This assessment needs JavaScript and WebAssembly, and this browser has no WebAssembly. " +
  "Run the same assessment with the shift2eu CLI instead: see the repository.";

// The blob URL of the last download, revoked when the next one starts.
let lastDownloadURL = null;

// guardFraming stops the page when another page frames it: a meta Content
// Security Policy cannot carry frame-ancestors. It also drops the page's own
// handle to the window that opened it.
function guardFraming() {
  if (window.top !== window.self) {
    return false;
  }
  window.opener = null;
  return true;
}

// download offers bytes as a file: a blob: URL is same-document and sends
// nothing. The URL of the previous download is revoked here, once the browser
// has had it.
function download(fileName, blob) {
  if (lastDownloadURL !== null) {
    URL.revokeObjectURL(lastDownloadURL);
  }
  const url = URL.createObjectURL(blob);
  lastDownloadURL = url;
  const link = document.createElement("a");
  link.download = fileName;
  link.href = url;
  link.click();
}

// unregisterServiceWorkers removes any service worker of this origin: the site
// registers none, so one that exists was not put there by a genuine page.
function unregisterServiceWorkers() {
  if (!("serviceWorker" in navigator)) {
    return;
  }
  navigator.serviceWorker.getRegistrations().then(function (registrations) {
    registrations.forEach(function (registration) {
      registration.unregister();
    });
  }).catch(function () {
    return null;
  });
}

// insertReport is the one place markup enters the page: the HTML reporter's
// fragment, through the Trusted Types policy.
function insertReport(target, html) {
  target.innerHTML = reportPolicy === null ? html : reportPolicy.createHTML(html);
}

// byId returns the element of the page with the id.
function byId(id) {
  return document.getElementById(id);
}

// showFramed replaces the page with why it stopped.
function showFramed() {
  document.body.textContent = "This page refuses to run inside a frame. " +
    "Open https://www.shift2eu.com in its own tab.";
}

// selectedTargets returns the target keys whose checkboxes are checked.
function selectedTargets() {
  const keys = [];
  document.querySelectorAll("input.target-choice").forEach(function (box) {
    if (box.checked) {
      keys.push(box.value);
    }
  });
  return keys;
}

// selectedRegions returns a region for each checked target that names one.
function selectedRegions() {
  const regions = [];
  document.querySelectorAll("input.target-choice").forEach(function (box) {
    const choice = byId("region-" + box.value);
    if (box.checked && choice !== null && choice.value !== "") {
      regions.push({ target: box.value, region: choice.value });
    }
  });
  return regions;
}

// startAssessment decodes the module from its script, starts it and wires the
// page's controls to it.
function startAssessment() {
  const notice = byId("loading-notice");
  if (typeof WebAssembly !== "object") {
    notice.textContent = noWebAssembly;
    return;
  }
  const encoded = globalThis.__shift2euModule;
  delete globalThis.__shift2euModule;
  if (typeof encoded !== "string") {
    notice.textContent = "The assessment module is missing from the page, so the assessment cannot run. Reload the page.";
    return;
  }
  const bytes = Uint8Array.from(atob(encoded), function (c) {
    return c.charCodeAt(0);
  });
  const go = new Go();
  WebAssembly.instantiate(bytes, go.importObject).then(function (instantiated) {
    go.run(instantiated.instance);
    if (typeof globalThis.shift2euAssess !== "function") {
      notice.textContent = "The assessment module did not start. Reload the page.";
      return;
    }
    notice.textContent = "The assessment module is loaded and runs in this browser.";
    wireControls();
  }).catch(function () {
    notice.textContent = "The assessment module failed to start in this browser. Run the assessment with the shift2eu CLI instead.";
  });
}

// wireControls makes the page's controls work once the module runs.
function wireControls() {
  const inventory = byId("inventory");
  const fileInput = byId("inventory-file");
  const run = byId("run");
  const status = byId("status");
  const report = byId("report");
  const markdownButton = byId("download-markdown");
  const jsonButton = byId("download-json");
  let inventoryName = "";
  let markdownBlob = null;
  let jsonBlob = null;

  inventory.addEventListener("input", function () {
    inventoryName = "";
  });

  fileInput.addEventListener("change", function () {
    const file = fileInput.files.item(0);
    if (file === null) {
      return;
    }
    file.text().then(function (text) {
      inventory.value = text;
      inventoryName = file.name;
    }).catch(function () {
      status.textContent = "The file could not be read.";
    });
  });

  byId("load-example").addEventListener("click", function () {
    const example = byId("example-" + byId("example").value);
    if (example === null) {
      return;
    }
    inventory.value = example.content.textContent;
    inventoryName = "inventory.json";
    fileInput.value = "";
  });

  markdownButton.addEventListener("click", function () {
    if (markdownBlob !== null) {
      download("shift2eu-report.md", markdownBlob);
    }
  });

  jsonButton.addEventListener("click", function () {
    if (jsonBlob !== null) {
      download("shift2eu-report.json", jsonBlob);
    }
  });

  run.addEventListener("click", function () {
    status.textContent = "";
    report.textContent = "";
    markdownBlob = null;
    jsonBlob = null;
    markdownButton.disabled = true;
    jsonButton.disabled = true;
    const options = {
      targets: selectedTargets(),
      regions: selectedRegions(),
      estimate: byId("estimate").checked,
      schemaVersion: "",
      inventoryName: inventoryName,
    };
    let result = null;
    try {
      result = globalThis.shift2euAssess(inventory.value, options);
    } catch (failure) {
      status.textContent = "The assessment failed unexpectedly; the page can run another one.";
      return;
    }
    if (typeof result.error === "string") {
      status.textContent = result.error;
      return;
    }
    markdownBlob = new Blob([result.markdown], { type: "text/markdown;charset=utf-8" });
    jsonBlob = new Blob([result.json], { type: "application/json" });
    insertReport(report, result.html);
    markdownButton.disabled = false;
    jsonButton.disabled = false;
    report.focus();
  });

  run.disabled = false;
}

// start runs on every page once it is parsed: the scripts are deferred.
function start() {
  if (!guardFraming()) {
    showFramed();
    return;
  }
  unregisterServiceWorkers();
  if (document.body.getAttribute("data-page") === "assessment") {
    startAssessment();
  }
}

start();
