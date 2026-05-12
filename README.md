tenant-observability-repo
Tenant-owned observability bundle. Per tenant, this repo ships:

PrometheusRule — alerting rules scoped to the tenant's app namespaces
AlertmanagerConfig — routes the tenant's alerts to its own webhook
tenant-alert-router (Deployment + Service) — webhook receiver that
forwards Alertmanager notifications to Telegram (and, later, Power
Automate / Microsoft Teams)
Secret containing Telegram bot token + chat ID

The platform's Argo CD (running in the infra repo's argocd namespace)
syncs this repo into the cluster.
Layout
tenant-observability-repo/
├── chart/                       # Helm chart — the deployable unit
│   ├── Chart.yaml
│   ├── values.yaml              # tenant identity, rules, Telegram creds
│   └── templates/
│       ├── _helpers.tpl
│       ├── namespace.yaml
│       ├── prometheusrule.yaml
│       ├── alertmanagerconfig.yaml
│       ├── telegram-secret.yaml
│       ├── router-deployment.yaml
│       └── router-service.yaml
├── tenant-alert-router/         # Source for the webhook service
│   ├── main.go
│   ├── go.mod
│   └── Dockerfile
└── argocd-application.yaml      # Drop into infra repo's monitoring/argocd/apps/
How an alert flows
Prometheus  → fires alert (rule from PrometheusRule CR)
   ↓
Alertmanager → routes via AlertmanagerConfig CR (namespace-scoped)
   ↓
webhook  → http://tenant-a-alert-router.tenant-a-monitoring-dev:80/webhook
   ↓
tenant-alert-router → formats message → Telegram Bot API
   ↓
Telegram channel
Onboarding a new tenant (5 minutes)

Fork or copy this repo, name it after the tenant.
Edit chart/values.yaml:

tenant.name → short tenant id (e.g. acme)
tenant.env → dev / stage / prod
prometheusRule.groups → tenant-specific alerts
telegram.botToken + telegram.chatId → real values (or use existingSecret)


Build & push the router image (see below) — or reuse the platform-built image.
Edit argocd-application.yaml:

spec.source.repoURL → your fork
spec.destination.namespace → <tenant>-monitoring-<env>


Commit argocd-application.yaml into the infra repo under
monitoring/argocd/apps/. Argo CD's root App-of-Apps will pick it up.

Building the router image
bashcd tenant-alert-router
docker build -t ghcr.io/YOUR_ORG/tenant-alert-router:0.1.0 .
docker push  ghcr.io/YOUR_ORG/tenant-alert-router:0.1.0
For CI: add a workflow in this repo that builds and pushes on tags.
Local testing
Run the router against a fake Alertmanager payload:
bashcd tenant-alert-router
TELEGRAM_BOT_TOKEN=xxx TELEGRAM_CHAT_ID=yyy TENANT=tenant-a go run .

# In another terminal:
curl -X POST http://localhost:8080/webhook \
  -H 'Content-Type: application/json' \
  -d '{
    "status":"firing",
    "receiver":"tenant-alert-router",
    "commonLabels":{"alertname":"TestAlert","severity":"warning","namespace":"txb-dev-customer-centric-dashboard"},
    "commonAnnotations":{"summary":"Smoke test","description":"Just checking the pipe"},
    "alerts":[{"status":"firing","labels":{},"annotations":{},"startsAt":"2026-05-12T12:00:00Z"}]
  }'
Troubleshooting
Alerts fire but nothing arrives in Telegram

kubectl -n tenant-a-monitoring-dev logs deploy/tenant-a-alert-router — look for telegram send failed.
Test the bot directly: curl "https://api.telegram.org/bot<TOKEN>/getMe".
Make sure the bot is a member (or admin) of the target channel.

AlertmanagerConfig appears to be ignored

Check the namespace has label alertmanagerConfig=enabled.
Confirm kubectl -n monitoring get alertmanager -o yaml shows
alertmanagerConfigSelector.matchLabels.alertmanagerConfig: enabled.
Reload Alertmanager:
kubectl -n monitoring rollout restart statefulset alertmanager-kube-prometheus-stack-alertmanager.

PrometheusRule appears to be ignored

kubectl -n monitoring get prometheus -o yaml | grep -A2 ruleSelector — should be empty ({}).
Check the CR exists: kubectl -n tenant-a-monitoring-dev get prometheusrule.
Visit Prometheus /rules page — your group should be listed.