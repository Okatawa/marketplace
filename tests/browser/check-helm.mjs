import fs from "node:fs";
import assert from "node:assert/strict";
import YAML from "yaml";
const docs = YAML.parseAllDocuments(fs.readFileSync(process.argv[2], "utf8"))
  .map((d) => {
    assert.equal(d.errors.length, 0);
    return d.toJSON();
  })
  .filter(Boolean);
const of = (kind) => docs.filter((d) => d.kind === kind);
if (process.argv.includes("--migrations-only")) {
  assert.equal(docs.length, 5);
  assert.equal(of("Job").length, 5, "migration phase must contain only Jobs");
  console.log("Helm: standalone migration phase contains only 5 Jobs");
  process.exit(0);
}
assert.equal(of("Deployment").length, 6);
assert.equal(of("Job").length, 5);
assert.equal(
  of("Secret").length,
  0,
  "secrets must be pre-created, never rendered",
);
for (const deployment of of("Deployment")) {
  const pod = deployment.spec.template.spec;
  assert.equal(pod.automountServiceAccountToken, false);
  assert.equal(pod.securityContext.runAsNonRoot, true);
  for (const c of pod.containers) {
    assert(c.readinessProbe && c.livenessProbe && c.startupProbe);
    assert(c.resources.requests.cpu && c.resources.limits.memory);
    assert.equal(c.securityContext.readOnlyRootFilesystem, true);
    assert(c.envFrom.some((e) => e.secretRef));
  }
}
for (const job of of("Job")) {
  const pod = job.spec.template.spec;
  assert.equal(pod.restartPolicy, "Never");
  assert.equal(pod.automountServiceAccountToken, false);
  assert.equal(pod.securityContext.runAsNonRoot, true);
  assert(job.spec.activeDeadlineSeconds > 0);
  for (const c of pod.containers) {
    assert.equal(c.securityContext.readOnlyRootFilesystem, true);
    assert.equal(c.securityContext.allowPrivilegeEscalation, false);
    assert(c.resources.requests.cpu && c.resources.limits.memory);
    assert(c.envFrom.every((e) => e.secretRef));
  }
}
for (const s of of("Service")) assert.equal(s.spec.type, "ClusterIP");
const ingress = of("Ingress");
assert.equal(ingress.length, 1);
assert(ingress[0].spec.tls[0].secretName);
assert.equal(
  ingress[0].spec.rules[0].http.paths[0].backend.service.name,
  "marketplace-gateway",
);
assert.equal(of("NetworkPolicy").length, 6);
for (const cm of of("ConfigMap")) {
  for (const key of Object.keys(cm.data))
    assert(
      !/PASSWORD|SECRET|TOKEN|PRIVATE_KEY|DATABASE_URL/.test(key),
      "credential in ConfigMap",
    );
}
console.log(
  "Helm: 6 deployments, 5 migration jobs, probes, security and ingress verified",
);
