const tokenInput = document.getElementById("tokenInput");
const authForm = document.getElementById("authForm");
const adminPanel = document.getElementById("adminPanel");
const sessionPill = document.getElementById("sessionPill");
const workflowSelect = document.getElementById("workflowSelect");
const workflowJsonInput = document.getElementById("workflowJsonInput");
const cppFilesInput = document.getElementById("cppFilesInput");
const cppFilesHint = document.getElementById("cppFilesHint");
const cppFilesList = document.getElementById("cppFilesList");
const topologyModeSelect = document.getElementById("topologyModeSelect");
const uploadBtn = document.getElementById("uploadBtn");
const activateBtn = document.getElementById("activateBtn");
const deleteBtn = document.getElementById("deleteBtn");
const refreshBtn = document.getElementById("refreshBtn");
const resetStateInput = document.getElementById("resetStateInput");
const clearOutputBtn = document.getElementById("clearOutputBtn");
const output = document.getElementById("output");
const statusLine = document.getElementById("statusLine");

let adminToken = "";
const selectedCppFiles = new Map();

function cppFileKey(file) {
  return `${file.name}::${file.size}::${file.lastModified}`;
}

function setBusy(button, busy, label) {
  button.disabled = busy;
  if (busy && label) {
    button.dataset.idleLabel ||= button.textContent;
    button.textContent = label;
  }
  if (!busy && button.dataset.idleLabel) {
    button.textContent = button.dataset.idleLabel;
  }
}

function show(value) {
  output.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
}

function setUnlocked(unlocked) {
  adminPanel.hidden = !unlocked;
  sessionPill.textContent = unlocked ? "Unlocked" : "Locked";
  sessionPill.classList.toggle("unlocked", unlocked);
}

function authHeaders(extra) {
  return Object.assign({}, extra || {}, {
    Authorization: `Bearer ${adminToken}`,
  });
}

async function api(url, init) {
  const response = await fetch(
    url,
    Object.assign({}, init || {}, {
      headers: authHeaders((init && init.headers) || {}),
    })
  );
  const text = await response.text();

  if (!response.ok) {
    throw new Error(`${response.status} ${text.trim()}`);
  }

  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function renderSelectedCppFiles() {
  const files = Array.from(selectedCppFiles.values());
  cppFilesList.innerHTML = "";

  if (files.length === 0) {
    cppFilesHint.textContent = "No C++ file selected";
    return;
  }

  cppFilesHint.textContent = `${files.length} C++ file(s) selected`;
  for (const file of files) {
    const item = document.createElement("li");
    item.textContent = file.name;
    cppFilesList.appendChild(item);
  }
}

function setWorkflowOptions(ids, preferredID) {
  workflowSelect.innerHTML = "";

  if (!ids || ids.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No workflow found";
    workflowSelect.appendChild(option);
    return;
  }

  for (const id of ids) {
    const option = document.createElement("option");
    option.value = id;
    option.textContent = id;
    workflowSelect.appendChild(option);
  }

  if (preferredID && ids.includes(preferredID)) {
    workflowSelect.value = preferredID;
  }
}

async function refreshWorkflows() {
  const data = await api("/api/admin/workflows");
  const preferredID = data.active_workflow_id || data.loaded_workflow_id || "";

  setWorkflowOptions(data.uploaded_ids || [], preferredID);
  topologyModeSelect.value = data.topology_mode || "plain";
  statusLine.textContent = `${preferredID || "No active workflow"} / ${data.topology_mode || "plain"}`;
  show(data);
}

async function unlock() {
  const token = tokenInput.value.trim();
  if (!token) {
    show("Token is required.");
    return;
  }

  adminToken = token;
  setBusy(document.getElementById("unlockBtn"), true, "Checking");

  try {
    await refreshWorkflows();
    setUnlocked(true);
    tokenInput.value = "";
    show("Admin unlocked.");
  } catch (err) {
    adminToken = "";
    setUnlocked(false);
    show(String(err));
  } finally {
    setBusy(document.getElementById("unlockBtn"), false);
  }
}

async function uploadWorkflow() {
  const workflowJSON = workflowJsonInput.files[0];
  const cppFiles = Array.from(selectedCppFiles.values());

  if (!adminToken) {
    show("Unlock first.");
    return;
  }
  if (!workflowJSON) {
    show("workflow_json file is required.");
    return;
  }
  if (cppFiles.length === 0) {
    show("At least one .cpp file is required.");
    return;
  }

  const form = new FormData();
  form.append("workflow_json", workflowJSON);
  for (const file of cppFiles) {
    form.append("cpp_files", file);
  }

  setBusy(uploadBtn, true, "Uploading");
  try {
    const data = await api("/api/admin/workflows/upload", {
      method: "POST",
      body: form,
    });
    show(data);
    workflowJsonInput.value = "";
    selectedCppFiles.clear();
    renderSelectedCppFiles();
    await refreshWorkflows();
  } catch (err) {
    show(String(err));
  } finally {
    setBusy(uploadBtn, false);
  }
}

async function activateWorkflow() {
  const workflowID = workflowSelect.value;

  if (!adminToken) {
    show("Unlock first.");
    return;
  }
  if (!workflowID) {
    show("Select a workflow.");
    return;
  }

  setBusy(activateBtn, true, "Activating");
  try {
    const data = await api("/api/admin/workflows/activate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        workflow_id: workflowID,
        reset_state: Boolean(resetStateInput.checked),
        topology_mode: topologyModeSelect.value || "plain",
      }),
    });
    show(data);
    await refreshWorkflows();
  } catch (err) {
    show(String(err));
  } finally {
    setBusy(activateBtn, false);
  }
}

async function deleteWorkflow() {
  const workflowID = workflowSelect.value;

  if (!adminToken) {
    show("Unlock first.");
    return;
  }
  if (!workflowID) {
    show("Select a workflow.");
    return;
  }
  if (!window.confirm(`Delete workflow ${workflowID}?`)) {
    return;
  }

  setBusy(deleteBtn, true, "Deleting");
  try {
    const data = await api("/api/admin/workflows/delete", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ workflow_id: workflowID }),
    });
    show(data);
    await refreshWorkflows();
  } catch (err) {
    show(String(err));
  } finally {
    setBusy(deleteBtn, false);
  }
}

authForm.addEventListener("submit", (event) => {
  event.preventDefault();
  void unlock();
});

cppFilesInput.addEventListener("change", () => {
  for (const file of cppFilesInput.files) {
    selectedCppFiles.set(cppFileKey(file), file);
  }
  cppFilesInput.value = "";
  renderSelectedCppFiles();
});

uploadBtn.addEventListener("click", () => {
  void uploadWorkflow();
});

activateBtn.addEventListener("click", () => {
  void activateWorkflow();
});

deleteBtn.addEventListener("click", () => {
  void deleteWorkflow();
});

refreshBtn.addEventListener("click", () => {
  void refreshWorkflows().catch((err) => show(String(err)));
});

clearOutputBtn.addEventListener("click", () => {
  show("Ready.");
});

renderSelectedCppFiles();
setUnlocked(false);
