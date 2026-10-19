"use strict";

function element(tag, text, className) {
	const node = document.createElement(tag);
	if (text !== undefined) node.textContent = text;
	if (className) node.className = className;
	return node;
}

function resolve(schema, spec) {
	if (!schema || typeof schema !== "object") return {};
	if (schema.$ref?.startsWith("#/")) {
		return (
			schema.$ref
				.slice(2)
				.split("/")
				.reduce(
					(value, key) => value?.[key.replace(/~1/g, "/").replace(/~0/g, "~")],
					spec,
				) || {}
		);
	}
	return schema;
}

function example(schema, spec, depth = 0) {
	if (depth > 7) return null;
	const value = resolve(schema, spec);
	if ("example" in value) return value.example;
	if ("const" in value) return value.const;
	if ("default" in value) return value.default;
	if (value.enum) return value.enum[0];
	if (value.type === "object" || value.properties) {
		return Object.fromEntries(
			Object.entries(value.properties || {}).map(([name, field]) => [
				name,
				example(field, spec, depth + 1),
			]),
		);
	}
	if (value.type === "array") return [example(value.items, spec, depth + 1)];
	if (value.type === "boolean") return true;
	if (value.type === "integer" || value.type === "number")
		return value.minimum ?? 0;
	if (value.type === "string") {
		if (value.format === "date-time") return "2026-01-01T00:00:00Z";
		return "example"
			.padEnd(Math.min(value.minLength || 0, 100), "x")
			.slice(0, value.maxLength ?? 100);
	}
	return null;
}

function pretty(value) {
	return JSON.stringify(value, null, 2);
}

function codeBlock(text, className = "") {
	const pre = element("pre", undefined, `code ${className}`);
	// Render token colors using text nodes, never interpret API content as HTML.
	const pattern =
		/("(?:\\.|[^"\\])*"\s*:?)|\b(true|false|null|-?\d+(?:\.\d+)?)\b/g;
	let offset = 0;
	for (const match of text.matchAll(pattern)) {
		pre.append(document.createTextNode(text.slice(offset, match.index)));
		const type = match[0].endsWith(":")
			? "json-key"
			: match[1]
				? "json-string"
				: "json-literal";
		pre.append(element("span", match[0], type));
		offset = match.index + match[0].length;
	}
	pre.append(document.createTextNode(text.slice(offset)));
	return pre;
}

function schemaTable(schema, spec) {
	const wrapper = element("div", undefined, "table-wrap");
	const table = element("table");
	const head = element("thead");
	const heading = element("tr");
	for (const title of ["Field", "Type", "Description / constraints"])
		heading.append(element("th", title));
	head.append(heading);
	const body = element("tbody");
	function row(name, schema, required, depth) {
		const value = resolve(schema, spec);
		const tr = element("tr");
		const field = element("td", name);
		field.append(element("small", required ? "required" : "optional"));
		const type =
			value.type || (value.const !== undefined ? typeof value.const : "any");
		const rules = [];
		if (value.description) rules.push(value.description);
		if (value.const !== undefined)
			rules.push(`Always ${JSON.stringify(value.const)}`);
		if (value.enum) rules.push(`One of: ${value.enum.join(", ")}`);
		if (value.minLength !== undefined)
			rules.push(`Min ${value.minLength} characters`);
		if (value.maxLength !== undefined)
			rules.push(`Max ${value.maxLength} characters`);
		if (value.minimum !== undefined) rules.push(`Minimum ${value.minimum}`);
		if (value.format) rules.push(value.format);
		if (value.additionalProperties === false) rules.push("No extra fields");
		tr.append(
			field,
			element("td", type, "field-type"),
			element("td", rules.join(" · ") || "—", "field-rules"),
		);
		body.append(tr);
		if (depth < 6) {
			for (const [key, child] of Object.entries(value.properties || {}))
				row(
					`${name}.${key}`,
					child,
					(value.required || []).includes(key),
					depth + 1,
				);
			if (value.items) row(`${name}[]`, value.items, false, depth + 1);
		}
	}
	const root = resolve(schema, spec);
	const entries = Object.entries(root.properties || {});
	if (entries.length) {
		for (const [name, child] of entries)
			row(name, child, (root.required || []).includes(name), 0);
	} else row("value", root, true, 0);
	table.append(head, body);
	wrapper.append(table);
	return wrapper;
}

function badge(method) {
	return element("span", method.toUpperCase(), `method method-${method}`);
}
function jsonSchema(content) {
	return content?.["application/json"]?.schema;
}

async function load() {
	const response = await fetch("/openapi/json");
	if (!response.ok)
		throw new Error(
			`Specification request failed (${response.status}). Reload to retry.`,
		);
	const spec = await response.json();
	document.title = `${spec.info.title} · API Reference`;
	document.querySelector("#version").textContent = `v${spec.info.version}`;
	const operations = [];
	for (const [path, paths] of Object.entries(spec.paths)) {
		for (const [method, operation] of Object.entries(paths)) {
			if (
				["get", "post", "put", "patch", "delete", "head", "options"].includes(
					method,
				)
			) {
				operations.push({
					path,
					method,
					operation,
					id: operation.operationId || `${method}-${path}`,
				});
			}
		}
	}
	const navigation = document.querySelector("#navigation");
	const detail = document.querySelector("#operation-detail");
	let selected;
	let pending;

	function select(entry) {
		if (pending) pending.abort();
		selected = entry;
		history.replaceState(null, "", `#${encodeURIComponent(entry.id)}`);
		for (const button of navigation.querySelectorAll("button"))
			button.setAttribute(
				"aria-current",
				button.dataset.id === entry.id ? "page" : "false",
			);
		detail.replaceChildren();
		const { path, method, operation } = entry;
		const protectedOperation = operation.security?.some((requirement) =>
			Object.hasOwn(requirement, "bearerAuth"),
		);
		const heading = element("header", undefined, "endpoint-heading");
		heading.append(
			element(
				"div",
				(operation.tags || ["Endpoints"]).join(" / ").toUpperCase(),
				"eyebrow",
			),
		);
		heading.append(element("h1", operation.summary || path));
		const endpointPath = element("div", undefined, "endpoint-path");
		endpointPath.append(badge(method), element("span", path));
		heading.append(endpointPath, element("p", operation.description || ""));
		const grid = element("div", undefined, "content-grid");
		const reference = element("div", undefined, "reference");
		if (protectedOperation) {
			const security = element("section", undefined, "section");
			security.append(
				element("h2", "Authentication"),
				element("p", "Bearer access token required."),
			);
			const permissions = operation["x-required-permissions"] || [];
			security.append(
				element(
					"p",
					permissions.length
						? `Required permissions (all): ${permissions.join(", ")}`
						: "Any authenticated identity is allowed.",
				),
			);
			reference.append(security);
		}
		const requestSection = element("section", undefined, "section");
		const requestTitle = element("h2", "Request body");
		const requestSchema = jsonSchema(operation.requestBody?.content);
		if (requestSchema) {
			requestTitle.append(element("span", "application/json", "chip"));
			requestSection.append(requestTitle, schemaTable(requestSchema, spec));
		} else
			requestSection.append(
				requestTitle,
				element("p", "This endpoint does not require a request body.", "muted"),
			);
		reference.append(requestSection);
		if (operation.parameters?.length) {
			const parameters = element("section", undefined, "section");
			parameters.append(element("h2", "Parameters"));
			for (const param of operation.parameters)
				parameters.append(
					element("h3", `${param.name} · ${param.in}`),
					schemaTable(param.schema, spec),
				);
			reference.append(parameters);
		}
		const responses = element("section", undefined, "section");
		responses.append(element("h2", "Responses"));
		const selectResponse = element("select", undefined, "response-select");
		selectResponse.setAttribute("aria-label", "Response status");
		for (const [status, value] of Object.entries(operation.responses || {})) {
			const option = element("option", `${status} — ${value.description}`);
			option.value = status;
			selectResponse.append(option);
		}
		const responseDetail = element("div");
		function showResponse() {
			responseDetail.replaceChildren();
			const response = operation.responses[selectResponse.value];
			const schema = jsonSchema(response?.content);
			if (schema)
				responseDetail.append(
					schemaTable(schema, spec),
					element("h3", "Example response"),
					codeBlock(pretty(example(schema, spec))),
				);
			else
				responseDetail.append(
					element("p", "No JSON response schema provided.", "muted"),
				);
		}
		selectResponse.addEventListener("change", showResponse);
		showResponse();
		responses.append(selectResponse, responseDetail);
		reference.append(responses);
		const raw = element("details");
		raw.append(
			element("summary", "View OpenAPI operation"),
			codeBlock(pretty(operation), "raw-schema"),
		);
		reference.append(raw);

		const consolePanel = element("section", undefined, "console");
		const consoleHeader = element("div", undefined, "console-header");
		consoleHeader.append(
			element("h2", "Try this endpoint"),
			element("span", "LIVE REQUEST"),
		);
		const consoleBody = element("div", undefined, "console-body");
		const form = element("form");
		const pathLabel = element("label", "Request path");
		const pathInput = element("input");
		pathInput.value = path;
		pathInput.required = true;
		pathInput.setAttribute("aria-label", "Request path");
		pathLabel.append(pathInput);
		form.append(pathLabel);
		let tokenInput;
		if (protectedOperation) {
			tokenInput = element("input");
			tokenInput.type = "password";
			tokenInput.autocomplete = "off";
			tokenInput.spellcheck = false;
			tokenInput.setAttribute("aria-label", "Bearer access token");
			const tokenLabel = element("label", "Bearer access token");
			tokenLabel.append(tokenInput);
			form.append(
				tokenLabel,
				element(
					"p",
					"Token only, without Bearer. Kept in this form until you switch endpoints or reload; never saved to browser storage or included in the cURL preview. Use a test token over HTTPS (or local development).",
					"muted",
				),
			);
		}
		let body;
		const feedback = element("div", "", "request-feedback");
		feedback.setAttribute("role", "status");
		if (requestSchema) {
			body = element("textarea");
			body.value = pretty(example(requestSchema, spec));
			body.setAttribute("aria-label", "JSON request body");
			body.spellcheck = false;
			const labelRow = element("div", undefined, "label-row");
			const format = element("button", "Format JSON", "small-button");
			format.type = "button";
			format.addEventListener("click", () => {
				try {
					body.value = pretty(JSON.parse(body.value));
					feedback.textContent = "";
					updateCurl();
				} catch {
					feedback.textContent =
						"Invalid JSON. Check quotes, commas, and brackets.";
				}
			});
			labelRow.append(element("label", "JSON body"), format);
			form.append(labelRow, body);
		}
		const send = element("button", "Send request →", "primary");
		send.type = "submit";
		form.append(send, feedback);
		const result = element("div", undefined);
		result.setAttribute("aria-live", "polite");
		result.append(
			element(
				"div",
				"Send a request to see its response here.",
				"empty-result",
			),
		);
		const curlDetails = element("details");
		const curl = element("pre", "", "code");
		curlDetails.append(element("summary", "cURL command"), curl);
		const quote = (value) => `'${value.replace(/'/g, `'"'"'`)}'`;
		function updateCurl() {
			curl.textContent =
				`curl -X ${method.toUpperCase()} ${quote(location.origin + pathInput.value)}` +
				(protectedOperation ? ' -H "Authorization: Bearer $TOKEN"' : "") +
				(body
					? ` \\\n  -H 'Content-Type: application/json' \\\n  --data-raw ${quote(body.value)}`
					: "");
		}
		pathInput.addEventListener("input", updateCurl);
		if (body) body.addEventListener("input", updateCurl);
		updateCurl();
		form.addEventListener("submit", async (event) => {
			event.preventDefault();
			feedback.textContent = "";
			result.replaceChildren(
				element("div", "Preparing request…", "empty-result"),
			);
			const controller = new AbortController();
			pending = controller;
			const timeout = setTimeout(() => controller.abort(), 30000);
			const started = performance.now();
			send.disabled = true;
			send.textContent = "Sending…";
			try {
				const url = new URL(pathInput.value, location.origin);
				if (
					url.origin !== location.origin ||
					!pathInput.value.startsWith("/") ||
					url.username ||
					url.password
				)
					throw new Error(
						"Enter a request path on this server, such as /health.",
					);
				if (/[{}]/.test(pathInput.value))
					throw new Error(
						"Replace path parameters with actual values before sending.",
					);
				const init = {
					method: method.toUpperCase(),
					credentials: "same-origin",
					signal: controller.signal,
					redirect: "error",
					headers: {},
				};
				if (body) {
					try {
						JSON.parse(body.value);
					} catch {
						throw new Error(
							"Invalid JSON. Check quotes, commas, and brackets.",
						);
					}
					init.headers["Content-Type"] = "application/json";
					init.body = body.value;
				}
				if (tokenInput?.value) {
					if (
						location.protocol !== "https:" &&
						!["localhost", "127.0.0.1", "[::1]"].includes(location.hostname)
					)
						throw new Error("Use HTTPS before sending a bearer token.");
					init.headers.Authorization = `Bearer ${tokenInput.value}`;
				}
				const res = await fetch(url, init);
				const text = await res.text();
				let formatted = text || "(empty response)";
				try {
					formatted = pretty(JSON.parse(text));
				} catch {
					/* Preserve non-JSON responses as text. */
				}
				const summary = element("div", undefined, "response-summary");
				summary.append(
					element(
						"span",
						`${res.status} ${res.statusText}`,
						`response-status ${res.ok ? "success" : "failure"}`,
					),
					element("span", `${Math.round(performance.now() - started)} ms`),
				);
				const headers = element("details");
				headers.append(
					element("summary", "Response headers"),
					element(
						"pre",
						Array.from(res.headers)
							.map(([key, value]) => `${key}: ${value}`)
							.join("\n"),
						"code",
					),
				);
				result.replaceChildren(
					summary,
					codeBlock(formatted, "result"),
					headers,
				);
			} catch (error) {
				feedback.textContent =
					error.name === "AbortError"
						? "Request canceled or timed out after 30 seconds."
						: error.message;
			} finally {
				clearTimeout(timeout);
				if (pending === controller) pending = null;
				send.disabled = false;
				send.textContent = "Send request →";
			}
		});
		consoleBody.append(form, result, curlDetails);
		consolePanel.append(consoleHeader, consoleBody);
		grid.append(reference, consolePanel);
		detail.append(heading, grid);
	}

	function filter() {
		const query = document.querySelector("#search").value.toLowerCase().trim();
		navigation.replaceChildren();
		const matches = operations.filter(({ path, method, operation }) =>
			`${path} ${method} ${operation.summary} ${(operation.tags || []).join(" ")}`
				.toLowerCase()
				.includes(query),
		);
		const groups = new Map();
		for (const entry of matches) {
			const tag = entry.operation.tags?.[0] || "Endpoints";
			if (!groups.has(tag)) groups.set(tag, []);
			groups.get(tag).push(entry);
		}
		for (const [tag, entries] of groups) {
			navigation.append(element("div", tag, "nav-group"));
			for (const entry of entries) {
				const button = element("button", undefined, "nav-item");
				button.type = "button";
				button.dataset.id = entry.id;
				button.setAttribute(
					"aria-current",
					selected === entry ? "page" : "false",
				);
				button.append(
					badge(entry.method),
					element("span", entry.path, "nav-label"),
				);
				button.addEventListener("click", () => select(entry));
				navigation.append(button);
			}
		}
		document.querySelector("#status").textContent = matches.length
			? `${matches.length} endpoint${matches.length === 1 ? "" : "s"}`
			: "No matching endpoints. Try another search.";
	}
	document.querySelector("#search").addEventListener("input", filter);
	filter();
	const initial =
		operations.find(
			(entry) => `#${encodeURIComponent(entry.id)}` === location.hash,
		) || operations[0];
	if (initial) select(initial);
	else
		detail.replaceChildren(
			element("div", "No endpoints are documented yet.", "empty-state"),
		);
}
load().catch((error) => {
	document.querySelector("#status").textContent = error.message;
	document
		.querySelector("#operation-detail")
		.replaceChildren(
			element(
				"div",
				"Unable to load the API reference. Reload the page to retry.",
				"empty-state",
			),
		);
});
