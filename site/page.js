// shift2eu's page script (PRD #297). Every page loads it. It sends nothing:
// test/site scans it for every browser API that can send data, load a
// resource or reach another window, and refuses any computed property name.
// The framing guard, the download function, the unregister function, the
// report's insertion point and the inventory page's clipboard write
// (copyCommand) are the only exceptions, each in its own function.
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
    notice.textContent = "The assessment engine is missing from the page, so the assessment cannot run. Reload the page.";
    return;
  }
  const bytes = Uint8Array.from(atob(encoded), function (c) {
    return c.charCodeAt(0);
  });
  const go = new Go();
  WebAssembly.instantiate(bytes, go.importObject).then(function (instantiated) {
    go.run(instantiated.instance);
    if (typeof globalThis.shift2euAssess !== "function") {
      notice.textContent = "The assessment engine did not start. Reload the page.";
      return;
    }
    // Once the engine runs, the notice is no box any more (issue #373, item
    // 03-1): it stays a status that a screen reader announces, and the
    // enabled Run button is what a reader sees.
    notice.textContent = "The assessment engine is ready and runs in this browser.";
    notice.classList.add("visually-hidden");
    wireControls();
  }).catch(function () {
    // wireControls may throw after the notice was hidden: show it again, so
    // a sighted reader sees the failure too.
    notice.classList.remove("visually-hidden");
    notice.textContent = "The assessment engine failed to start in this browser. Run the assessment with the shift2eu CLI instead.";
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
    // The module answers an object; anything else carries no assessment and no
    // message, so the page shows one constant one and stays usable.
    if (result === null || typeof result !== "object") {
      status.textContent = "The assessment gave no result; the page can run another one.";
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

// copyCommand puts the command beside button on the clipboard: the one place
// the page writes to it, on a click only, and it never reads it. The command
// is the text of the <pre> the button sits beside, as the page shows it. The
// status beside the button says "Copied", and the other buttons' statuses are
// cleared, so the last copy is the one shown. A copy fails and says so both
// when the write is refused and when the browser has no clipboard API, which
// throws before any promise exists; a failed status carries the class
// copy-failed, so it is never drawn as a success.
function copyCommand(button) {
  const box = button.parentElement;
  const command = box.querySelector("pre");
  const status = box.querySelector(".copy-status");
  document.querySelectorAll(".copy-status").forEach(function (other) {
    other.textContent = "";
    other.classList.remove("copy-failed");
  });
  const failed = function () {
    status.textContent = "Copy failed: select the command and copy it yourself.";
    status.classList.add("copy-failed");
  };
  try {
    navigator.clipboard.writeText(command.textContent).then(function () {
      status.textContent = "Copied";
    }).catch(failed);
  } catch {
    failed();
  }
}

// chooseSource shows the section of the chosen source button and hides the
// other sources' sections: the buttons say which is shown with aria-pressed.
// With no script every section stays shown.
function chooseSource(chosen) {
  document.querySelectorAll("button[aria-controls]").forEach(function (button) {
    const section = byId(button.getAttribute("aria-controls"));
    const pressed = button === chosen;
    button.ariaPressed = pressed ? "true" : "false";
    if (section !== null) {
      section.hidden = !pressed;
    }
  });
}

// wireInventoryPage makes the inventory page's source buttons and copy
// buttons work. A button's click is what a mouse, Enter and Space all fire.
// The page hides the source buttons until they work, so it shows them here,
// once they are wired: with no script they would do nothing.
function wireInventoryPage() {
  const sources = document.querySelectorAll("button[aria-controls]");
  if (sources.length === 0) {
    return;
  }
  const picker = document.querySelector(".source-picker");
  sources.forEach(function (button) {
    button.addEventListener("click", function () {
      chooseSource(button);
    });
    if (button.getAttribute("aria-pressed") === "true") {
      chooseSource(button);
    }
  });
  document.querySelectorAll("button.copy").forEach(function (button) {
    button.addEventListener("click", function () {
      copyCommand(button);
    });
  });
  if (picker !== null) {
    picker.hidden = false;
  }
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
  wireInventoryPage();
}

start();
