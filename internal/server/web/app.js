const toast = document.getElementById("toast");
const locationSelect = document.getElementById("locationSelect");
const activeAccountSelect = document.getElementById("activeAccountSelect");
const selectedLocationBadge = document.getElementById("selectedLocationBadge");
const resultModeBadge = document.getElementById("resultModeBadge");
const activeLeaseCard = document.getElementById("activeLeaseCard");
const awsCliBadge = document.getElementById("awsCliBadge");
const awsCliHelp = document.getElementById("awsCliHelp");
const awsCliProfileSelect = document.getElementById("awsCliProfileSelect");
const awsCliExportCard = document.getElementById("awsCliExportCard");
const provisionButton = document.getElementById("provisionButton");
const cleanupAllButton = document.getElementById("cleanupAllButton");
const cleanupResultCard = document.getElementById("cleanupResultCard");
const activeAccountStorageKey = "egress.activeAccountId";

const state = {
  selectedLocation: null,
  activeAccountId: window.localStorage.getItem(activeAccountStorageKey) || "",
  accessMode: "proxy",
  dashboard: null,
  lastImportedEnv: "",
  provisionState: "idle",
  provisionMessage: "",
  lastLease: null,
  lastCleanupResult: null,
  localConnectState: "idle",
  localConnectMessage: "",
  localConnected: false
};

async function fetchDashboard() {
  const response = await fetch("/api/dashboard");
  if (!response.ok) {
    throw new Error("Failed to load dashboard");
  }
  return response.json();
}

async function postJSON(url, payload) {
  const response = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  const data = await response.json();
  if (!response.ok) {
    throw new Error(data.error || "Request failed");
  }
  return data;
}

function setToast(message, isError = false) {
  toast.textContent = message;
  toast.style.background = isError ? "rgba(153, 27, 27, 0.96)" : "rgba(25, 33, 38, 0.95)";
  toast.classList.remove("hidden");
  window.clearTimeout(setToast.timer);
  setToast.timer = window.setTimeout(() => toast.classList.add("hidden"), 2600);
}

function countSummary(data) {
  return [
    { label: "Cities online", value: data.supported_locations.length },
    { label: "Cloud accounts", value: data.accounts.length },
    { label: "Policies hidden underneath", value: Object.keys(data.policies).length },
    { label: "Active leases", value: data.leases.length }
  ];
}

function renderHeroStats(data) {
  const container = document.getElementById("heroStats");
  container.innerHTML = countSummary(data)
    .map(
      (item) => `
        <article class="stat-card">
          <span class="kicker">${item.label}</span>
          <strong class="stat-value">${item.value}</strong>
        </article>
      `
    )
    .join("");
}

function renderLocationSelect(locations) {
  locationSelect.innerHTML = locations
    .map((location) => {
      const selected = state.selectedLocation === location.id ? "selected" : "";
      return `<option value="${location.id}" ${selected}>${location.name} · ${location.region}</option>`;
    })
    .join("");
}

function syncSelectedLocation() {
  const location = state.dashboard.supported_locations.find((item) => item.id === state.selectedLocation);
  selectedLocationBadge.textContent = location ? `${location.name} · ${location.region}` : "No city selected";
}

function renderAccounts(accounts) {
  const el = document.getElementById("accountsList");
  if (!accounts.length) {
    el.innerHTML = '<div class="empty">No AWS account saved yet.</div>';
    return;
  }
  el.innerHTML = accounts
    .map(
      (account) => `
        <article class="item-card">
          <header>
            <h3>${account.name}</h3>
            <span class="badge">${account.status}</span>
          </header>
          <div class="meta">
            <span>${account.provider.toUpperCase()}</span>
            <span class="mono">${account.aws_account_id}</span>
            <span>${account.credential_source || "manual"}</span>
          </div>
          <p class="lede mono">${account.aws_profile || account.role_arn || account.principal_arn || ""}</p>
        </article>
      `
    )
    .join("");
}

function renderAWSCLI(status) {
  awsCliBadge.textContent = status.available ? "AWS CLI detected" : "AWS CLI unavailable";
  awsCliHelp.textContent = status.available
    ? "Pick a local AWS profile and import verified credentials through the AWS CLI."
    : status.error || "AWS CLI is not available on the server.";

  if (!status.available || !status.profiles.length) {
    awsCliProfileSelect.innerHTML = '<option value="">No AWS profiles found</option>';
    awsCliProfileSelect.disabled = true;
    return;
  }

  awsCliProfileSelect.disabled = false;
  awsCliProfileSelect.innerHTML = status.profiles
    .map((profile) => `<option value="${profile}">${profile}</option>`)
    .join("");
}

function usableAccounts(accounts) {
  return accounts.filter((account) => account.provider === "aws" && account.credential_source === "aws_cli" && account.status === "connected");
}

function renderActiveAccountSelect(accounts) {
  const usable = usableAccounts(accounts);
  if (!usable.length) {
    activeAccountSelect.innerHTML = '<option value="">No AWS CLI account connected</option>';
    activeAccountSelect.disabled = true;
    provisionButton.disabled = true;
    cleanupAllButton.disabled = true;
    state.activeAccountId = "";
    window.localStorage.removeItem(activeAccountStorageKey);
    return;
  }
  const active = usable.find((account) => account.id === state.activeAccountId) || usable[0];
  state.activeAccountId = active.id;
  window.localStorage.setItem(activeAccountStorageKey, active.id);
  activeAccountSelect.disabled = false;
  provisionButton.disabled = false;
  cleanupAllButton.disabled = false;
  activeAccountSelect.innerHTML = usable
    .map((account) => `<option value="${account.id}" ${account.id === active.id ? "selected" : ""}>${account.name} · ${account.aws_account_id}</option>`)
    .join("");
}

function renderAWSCLIExport() {
  if (!state.lastImportedEnv) {
    awsCliExportCard.className = "empty";
    awsCliExportCard.innerHTML = "Import a profile to get a copyable AWS env export block.";
    return;
  }

  awsCliExportCard.className = "connection-card";
  awsCliExportCard.innerHTML = `
    <div class="connection-header">
      <div>
        <p class="eyebrow">AWS CLI export</p>
        <h3>Temporary credentials</h3>
      </div>
    </div>
    <div class="command-block">
      <span class="kicker">Shell env</span>
      <pre>${state.lastImportedEnv}</pre>
    </div>
  `;
}

function renderCleanupResult() {
  if (!state.lastCleanupResult) {
    cleanupResultCard.className = "empty";
    cleanupResultCard.innerHTML = "Cleanup results will appear here.";
    return;
  }

  cleanupResultCard.className = "connection-card";
  cleanupResultCard.innerHTML = `
    <div class="connection-header">
      <div>
        <p class="eyebrow">Cleanup result</p>
        <h3>${state.lastCleanupResult.scope === "account" ? "Selected account cleaned up" : "Cleanup completed"}</h3>
      </div>
    </div>
    <div class="command-block">
      <span class="kicker">Summary</span>
      <pre>${state.lastCleanupResult.detail || "Cleanup completed."}</pre>
    </div>
    <div class="command-block">
      <span class="kicker">Output</span>
      <pre>${JSON.stringify(state.lastCleanupResult, null, 2)}</pre>
    </div>
  `;
}

function renderLeases(leases) {
  const el = document.getElementById("leasesList");
  if (!leases.length) {
    el.innerHTML = '<div class="empty">No leases yet.</div>';
    return;
  }
  el.innerHTML = leases
    .slice()
    .sort((a, b) => new Date(b.issued_at) - new Date(a.issued_at))
    .map(
      (lease) => `
        <article class="item-card">
          <header>
            <h3>${lease.location || lease.region}</h3>
            <span class="badge">${lease.access_mode || "proxy"}</span>
          </header>
          <div class="meta">
            <span class="mono">${lease.public_ip}</span>
            <span>${lease.region}</span>
            <span>expires ${new Date(lease.expires_at).toLocaleTimeString()}</span>
          </div>
          <p class="lede mono">${lease.connection.client_endpoint || lease.endpoint}</p>
        </article>
      `
    )
    .join("");
}

function renderActiveLease(lease) {
  if (state.provisionState === "pending") {
    resultModeBadge.textContent = "PROVISIONING";
    activeLeaseCard.className = "connection-card";
    activeLeaseCard.innerHTML = `
      <div class="connection-header">
        <div>
          <p class="eyebrow">Provisioning</p>
          <h3>Creating ${state.accessMode.toUpperCase()} endpoint in AWS</h3>
        </div>
      </div>
      <div class="command-block">
        <span class="kicker">Status</span>
        <pre>${state.provisionMessage || "Waiting for AWS resources to become ready..."}</pre>
      </div>
    `;
    return;
  }

  if (state.provisionState === "error") {
    resultModeBadge.textContent = "FAILED";
    activeLeaseCard.className = "connection-card";
    activeLeaseCard.innerHTML = `
      <div class="connection-header">
        <div>
          <p class="eyebrow">Failed</p>
          <h3>Provisioning did not complete</h3>
        </div>
      </div>
      <div class="command-block">
        <span class="kicker">AWS error</span>
        <pre>${state.provisionMessage}</pre>
      </div>
    `;
    return;
  }

  if (!lease) {
    resultModeBadge.textContent = "Waiting";
    activeLeaseCard.className = "empty";
    activeLeaseCard.innerHTML = "Pick a location and provision an egress endpoint.";
    return;
  }

  resultModeBadge.textContent = lease.status ? lease.status.toUpperCase() : lease.access_mode.toUpperCase();
  activeLeaseCard.className = "connection-card";

  const details =
    lease.access_mode === "vpn"
      ? `
        <div class="connection-grid">
          <div>
            <span class="kicker">Endpoint</span>
            <div class="mono">${lease.connection.client_endpoint}</div>
          </div>
          <div>
            <span class="kicker">Download</span>
            <div class="mono">${lease.connection.download_url}</div>
          </div>
        </div>
        <div class="command-block">
          <span class="kicker">Quick setup</span>
          <pre>${lease.connection.setup_command}</pre>
        </div>
        <div class="command-block">
          <span class="kicker">Automatic local connect</span>
          <button id="connectLocalButton" type="button" class="primary-action">Connect this machine</button>
          <button id="disconnectLocalButton" type="button" class="primary-action">Disconnect and clean up</button>
          <pre>${state.localConnectMessage || "Uses sudo on this machine to install /etc/wireguard/wg0.conf and bring wg0 up."}</pre>
        </div>
        <div class="command-block">
          <span class="kicker">VPN config</span>
          <pre>${lease.connection.vpn_config}</pre>
        </div>
        <div class="command-block">
          <span class="kicker">AWS resources</span>
          <pre>Instance: ${lease.resources?.instance_id || "n/a"}
Security Group: ${lease.resources?.security_group_id || "n/a"}
Elastic IP: ${lease.resources?.allocation_id || "n/a"}
VPC: ${lease.resources?.vpc_id || "n/a"}
Subnet: ${lease.resources?.subnet_id || "n/a"}</pre>
        </div>
      `
      : `
        <div class="connection-grid">
          <div>
            <span class="kicker">Proxy URL</span>
            <div class="mono">${lease.connection.proxy_url}</div>
          </div>
          <div>
            <span class="kicker">Public IP</span>
            <div class="mono">${lease.public_ip}</div>
          </div>
        </div>
        <div class="command-block">
          <span class="kicker">Quick setup</span>
          <pre>${lease.connection.setup_command}</pre>
        </div>
        <div class="command-block">
          <span class="kicker">Environment</span>
          <pre>${Object.entries(lease.connection.env || {})
            .map(([key, value]) => `${key}=${value}`)
            .join("\n")}</pre>
        </div>
        <div class="command-block">
          <span class="kicker">AWS resources</span>
          <pre>Instance: ${lease.resources?.instance_id || "n/a"}
Security Group: ${lease.resources?.security_group_id || "n/a"}
Elastic IP: ${lease.resources?.allocation_id || "n/a"}
VPC: ${lease.resources?.vpc_id || "n/a"}
Subnet: ${lease.resources?.subnet_id || "n/a"}</pre>
        </div>
      `;

  activeLeaseCard.innerHTML = `
    <div class="connection-header">
      <div>
        <p class="eyebrow">Ready</p>
        <h3>${lease.location || lease.region}</h3>
      </div>
      <div class="meta">
        <span>${lease.region}</span>
        <span>${new Date(lease.expires_at).toLocaleString()}</span>
      </div>
    </div>
    ${details}
  `;

  if (lease.access_mode === "vpn") {
    const connectButton = document.getElementById("connectLocalButton");
    const disconnectButton = document.getElementById("disconnectLocalButton");
    if (connectButton) {
      connectButton.disabled = state.localConnectState === "pending" || state.localConnected;
      connectButton.addEventListener("click", async () => {
        try {
          state.localConnectState = "pending";
          state.localConnectMessage = "Applying WireGuard config to this machine...";
          renderActiveLease(lease);
          const result = await postJSON("/api/connect-local", { lease_id: lease.id });
          state.localConnectState = "connected";
          state.localConnected = true;
          state.localConnectMessage = result.detail || "wg0 connected on this machine";
          await bootstrap();
          setToast("Local VPN connected");
        } catch (error) {
          state.localConnectState = "error";
          state.localConnectMessage = error.message;
          renderActiveLease(lease);
          setToast(error.message, true);
        }
      });
    }
    if (disconnectButton) {
      disconnectButton.disabled = state.localConnectState === "pending";
      disconnectButton.addEventListener("click", async () => {
        try {
          state.localConnectState = "pending";
          state.localConnectMessage = "Cleaning up the local tunnel and destroying the AWS gateway...";
          renderActiveLease(lease);
          const result = await postJSON("/api/disconnect-local", { lease_id: lease.id });
          state.localConnectState = "idle";
          state.localConnected = false;
          state.localConnectMessage = result.detail || "wg0 disconnected on this machine";
          await bootstrap();
          setToast("Local VPN disconnected and gateway cleaned up");
        } catch (error) {
          state.localConnectState = "error";
          state.localConnectMessage = error.message;
          renderActiveLease(lease);
          setToast(error.message, true);
        }
      });
    }
  }
}

function renderAll(data) {
  state.dashboard = data;
  state.localConnected = Boolean(data.local_vpn?.connected);
  if (state.localConnectState === "idle" || state.localConnectState === "connected") {
    state.localConnectMessage = data.local_vpn?.detail || state.localConnectMessage;
  }
  if (!state.selectedLocation && data.supported_locations.length) {
    state.selectedLocation = data.supported_locations[0].id;
  }
  renderHeroStats(data);
  renderLocationSelect(data.supported_locations);
  syncSelectedLocation();
  renderAWSCLI(data.aws_cli);
  renderAWSCLIExport();
  renderActiveAccountSelect(data.accounts);
  renderCleanupResult();
  renderAccounts(data.accounts);
  renderLeases(data.leases);
  const latestLease = data.leases.slice().sort((a, b) => new Date(b.issued_at) - new Date(a.issued_at))[0];
  if (state.provisionState !== "pending" && state.provisionState !== "error") {
    state.lastLease = latestLease || null;
  }
  renderActiveLease(state.lastLease || latestLease);
}

function attachModeSwitch() {
  document.querySelectorAll(".mode-chip").forEach((button) => {
    button.addEventListener("click", () => {
      state.accessMode = button.dataset.mode;
      document.querySelectorAll(".mode-chip").forEach((chip) => {
        chip.classList.toggle("active", chip.dataset.mode === state.accessMode);
      });
    });
  });
}

function attachForms() {
  locationSelect.addEventListener("change", (event) => {
    state.selectedLocation = event.target.value;
    syncSelectedLocation();
  });
  activeAccountSelect.addEventListener("change", (event) => {
    state.activeAccountId = event.target.value;
    window.localStorage.setItem(activeAccountStorageKey, state.activeAccountId);
  });

  document.getElementById("accountForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    try {
      await postJSON("/api/accounts", {
        name: form.get("name"),
        provider: "aws",
        aws_account_id: form.get("aws_account_id"),
        role_arn: form.get("role_arn"),
        external_id: form.get("external_id")
      });
      setToast("AWS account saved");
      event.currentTarget.reset();
      await bootstrap();
    } catch (error) {
      setToast(error.message, true);
    }
  });

  document.getElementById("awsCliImportForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    try {
      const result = await postJSON("/api/accounts/import-aws-cli", {
        profile: form.get("profile"),
        name: form.get("name")
      });
      state.lastImportedEnv = result.export_env || "";
      state.activeAccountId = result.account.id;
      window.localStorage.setItem(activeAccountStorageKey, state.activeAccountId);
      renderAWSCLIExport();
      setToast(`Imported AWS profile ${result.account.aws_profile}`);
      await bootstrap();
    } catch (error) {
      setToast(error.message, true);
    }
  });

  document.getElementById("provisionForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!state.selectedLocation) {
      setToast("Pick a location first", true);
      return;
    }
    const form = new FormData(event.currentTarget);
    try {
      state.provisionState = "pending";
      state.provisionMessage = `Deploying ${state.accessMode.toUpperCase()} infrastructure in ${state.selectedLocation}. This can take around a minute.`;
      renderActiveLease(null);
      if (!state.activeAccountId) {
        throw new Error("Select an active AWS account first");
      }
      const lease = await postJSON("/api/provision", {
        account_id: state.activeAccountId,
        location_id: state.selectedLocation,
        access_mode: state.accessMode,
        workload_id: form.get("workload_id")
      });
      state.provisionState = "ready";
      state.provisionMessage = "";
      state.lastLease = lease;
      state.localConnectState = "idle";
      state.localConnectMessage = "";
      setToast(`${lease.access_mode.toUpperCase()} provisioned in AWS for ${lease.location}`);
      renderActiveLease(lease);
      await bootstrap();
    } catch (error) {
      state.provisionState = "error";
      state.provisionMessage = error.message;
      renderActiveLease(null);
      setToast(error.message, true);
    }
  });

  cleanupAllButton.addEventListener("click", async () => {
    cleanupAllButton.disabled = true;
    try {
      if (!state.activeAccountId) {
        throw new Error("Select an active AWS account first");
      }
      const result = await postJSON("/api/cleanup-all", { account_id: state.activeAccountId });
      state.lastCleanupResult = result;
      state.localConnectState = "idle";
      state.localConnected = false;
      state.localConnectMessage = "All app-managed AWS egress resources have been cleaned up.";
      await bootstrap();
      setToast(result.detail || "All AWS egress resources cleaned up");
    } catch (error) {
      setToast(error.message, true);
    } finally {
      cleanupAllButton.disabled = false;
    }
  });
}

async function bootstrap() {
  const data = await fetchDashboard();
  renderAll(data);
}

attachModeSwitch();
attachForms();
bootstrap().catch((error) => setToast(error.message, true));
