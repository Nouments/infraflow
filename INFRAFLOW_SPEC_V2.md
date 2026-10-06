# InfraFlow — Cahier des charges opérationnel V2

> **Document de référence exécutable pour le développement, les agents Codex, les démonstrations et la validation M2.**
>
> Version V2 — 06/10/2026
>
> **Objectif absolu : construire un InfraFlow réellement démontrable avec le matériel, les VM, les appliances et les logiciels disponibles. Aucun écran, endpoint, adapter, état ou résultat ne doit être présenté comme réel s'il n'est pas alimenté par une source réelle.**

## 0. Règle d'or : REAL-ONLY

InfraFlow doit distinguer strictement quatre états :

| Statut | Signification | Peut être présenté comme fonctionnel ? |
|---|---|---|
| `IMPLEMENTED` | Code présent et testé dans le dépôt | Oui, pour la partie testée |
| `LAB-VERIFIED` | Fonction testée sur un vrai lab / matériel / VM | Oui |
| `EXPERIMENTAL` | Fonction présente mais validation incomplète | Oui, uniquement avec badge expérimental |
| `UNVERIFIED` | Hypothèse, stub, fixture ou documentation seulement | Non |
| `UNSUPPORTED` | Explicitement non supporté | Non |

**Interdiction :** `fixture`, `mock`, `fake`, `demo data`, `TODO`, réponse statique ou donnée calculée uniquement côté frontend ne doit jamais être présentée comme un état réel d'infrastructure.

Chaque écran et chaque API doit pouvoir répondre à :

```text
SOURCE OF TRUTH
→ quelle donnée alimente cette information ?
→ quel device/agent/job l'a produite ?
→ quand a-t-elle été observée ?
→ est-elle désirée, observée, exécutée ou simulée ?
```

Si la réponse n'existe pas, afficher `UNKNOWN`, `UNVERIFIED` ou `NO DATA`, jamais une valeur inventée.

---

# 1. Objectif final du projet

InfraFlow doit devenir une plateforme d'orchestration d'infrastructure **réellement exécutable** :

```text
infra.yaml
   ↓
validation réelle
   ↓
normalisation
   ↓
capabilities réelles
   ↓
graphe / dépendances
   ↓
PLAN
   ↓
approbation selon policy
   ↓
JOB
   ↓
AGENT
   ↓
EXECUTION RÉELLE
   ↓
OBSERVED STATE RÉEL
   ↓
VERIFICATION
   ↓
RECONCILIATION
   ↓
TOPOLOGIE / DASHBOARD / TUI
   ↓
AUDIT
```

Le projet doit être capable de démontrer au minimum un workflow réel de bout en bout avec les ressources disponibles : Linux/Arch/Debian, Docker, GNS3/EVE-NG, Cisco IOS/IOS-XE disponibles, MikroTik disponible, FortiGate disponible si le lab est opérationnel, Proxmox si disponible.

Le système doit préférer **ne pas exécuter** plutôt que d'inventer une capacité.

---

# 2. Contrat obligatoire pour tous les agents de développement

Avant toute modification, l'agent DOIT :

1. lire `README.md` ;
2. lire `ARCHITECTURE.md` ;
3. lire `INFRAFLOW_SPEC.md` ;
4. inspecter l'arborescence réelle ;
5. rechercher l'implémentation existante ;
6. rechercher les tests ;
7. rechercher les TODO/stubs/mocks ;
8. vérifier les interfaces existantes ;
9. vérifier les endpoints existants ;
10. vérifier le modèle de données existant ;
11. vérifier les dépendances ;
12. vérifier si la feature est déjà partiellement implémentée ;
13. ne créer aucune seconde implémentation concurrente ;
14. exécuter les tests concernés avant modification si possible.

Après modification, l'agent DOIT fournir :

```text
STATUS: IMPLEMENTED | LAB-VERIFIED | EXPERIMENTAL | UNVERIFIED | BLOCKED

Implemented:
- ...

Real test executed:
- command / lab / device / result

Not implemented:
- ...

Known limitations:
- ...

Files changed:
- ...

Tests:
- unit: ...
- integration: ...
- e2e/lab: ...

No-fabrication check:
- no fake state added: yes/no

Next task:
- ...
```

---

# 3. Objectif de démonstration M2

La démonstration ne doit pas être une vidéo de données préchargées. L'encadreur doit pouvoir voir une action réelle provoquer un changement réel.

## Scénario cible

```text
1. démarrer InfraFlow Provider
2. démarrer InfraFlow Agent
3. ouvrir Dashboard
4. ouvrir TUI
5. charger `examples/demo-real.yaml`
6. valider
7. afficher le plan
8. approuver le job
9. générer les artifacts
10. démarrer le bootstrap
11. voir l'agent réel recevoir le job
12. voir les tâches réelles progresser
13. voir les logs réels
14. voir le device réel changer
15. vérifier le device
16. recevoir l'Observed State
17. afficher la topologie réelle
18. modifier volontairement un paramètre hors InfraFlow
19. relancer reconciliation
20. afficher DRIFT
21. corriger via InfraFlow
22. vérifier retour HEALTHY
23. couper le controller
24. démontrer la continuité locale si cette capacité est déclarée LAB-VERIFIED
25. reconnecter
26. synchroniser les événements
27. afficher l'audit complet
```

**Si une étape n'est pas réellement opérationnelle, elle doit être explicitement marquée dans la démonstration et non maquillée.**

---

# 4. Architecture cible figée

```text
                       ┌──────────────────────────┐
                       │        WEB DASHBOARD     │
                       │ real state / topology    │
                       │ jobs / logs / audit      │
                       └────────────┬─────────────┘
                                    │ HTTPS + SSE/WSS
                       ┌────────────▼─────────────┐
                       │        CONTROLLER        │
                       │ API / Auth / RBAC        │
                       │ Planner / Scheduler      │
                       │ Job Engine / Reconcile   │
                       │ State / Event / Audit    │
                       └────────────┬─────────────┘
                                    │ mTLS/gRPC
                       ┌────────────▼─────────────┐
                       │       SITE AGENT         │
                       │ local state / queue      │
                       │ executor / adapters     │
                       │ DHCP DNS TFTP HTTP PXE  │
                       │ TUI / health / sync     │
                       └────────────┬─────────────┘
                                    │
             ┌──────────────────────┼──────────────────────┐
             ▼                      ▼                      ▼
         Cisco IOS/XE           MikroTik              FortiGate
         Switch/Router          RouterOS              Firewall
             │                      │                      │
             └──────────────────────┼──────────────────────┘
                                    ▼
                         Proxmox / Linux / PCs
```

Le domaine et l'orchestrateur ne doivent dépendre d'aucun constructeur.

---

# 5. Cycle de vie exact d'une feature

Aucune feature ne passe directement de `TODO` à `SUPPORTED`.

```text
SPECIFIED
   ↓
IMPLEMENTED
   ↓
UNIT-TESTED
   ↓
INTEGRATION-TESTED
   ↓
LAB-VERIFIED
   ↓
SUPPORTED
```

Pour un vendor :

```text
vendor/model/version
 + provisioning method
 + connection method
 + verification method
```

doivent tous être connus.

Exemple correct :

```yaml
vendor: cisco
family: ios-xe
model: csr1000v
method: ssh
status: LAB-VERIFIED
lab:
  platform: GNS3
  image: <actual-image-used>
  test_date: 2026-10-06
```

Exemple incorrect :

```yaml
vendor: cisco
status: supported
```

---

# 6. Source de vérité et modèles d'état

InfraFlow doit maintenir quatre états distincts :

```text
DESIRED STATE       = ce que l'utilisateur demande
EXECUTION STATE     = ce que le job est en train de faire
OBSERVED STATE      = ce qui a réellement été observé
AUDIT STATE         = ce qui s'est réellement produit historiquement
```

Un écran ne doit jamais mélanger les quatre.

## Exemple Dashboard

```text
DESIRED
R1 hostname = R1

OBSERVED
R1 hostname = R1
observed_at = 22:10:31
source = agent-site-01

EXECUTION
job-42 / verify / 100%

AUDIT
22:09 configuration_started
22:10 configuration_completed
```

---

# 7. YAML : comportement réel obligatoire

Le YAML doit être accepté uniquement si :

- syntaxe valide ;
- schéma valide ;
- références valides ;
- CIDR valides ;
- pas de conflit IP ;
- pas de doublon MAC ;
- devices connus ou explicitement déclarés ;
- méthode de provisioning disponible ;
- capability compatible ;
- dépendances résolubles.

`validate` ne doit provoquer aucun effet de bord.

`plan` ne doit provoquer aucun effet de bord.

`apply` est la première opération autorisée à modifier l'infrastructure.

---

# 8. Planner : résultat obligatoire

Pour chaque plan, le backend doit produire :

```text
plan_id
project_id
site_id
created_at
input_hash
capability_resolution
ordered_tasks
dependencies
risk_level
required_approvals
blocked_tasks
unsupported_tasks
```

Exemple d'affichage réel :

```text
PLAN #42

1. verify-agent             READY
2. publish-dhcp             READY
3. bootstrap-R1             READY
4. verify-R1                BLOCKED BY 3
5. configure-R2             BLOCKED BY 4
6. verify-R2                BLOCKED BY 5
```

Le plan doit expliquer pourquoi une tâche est bloquée.

---

# 9. Job Engine : comportement réel

Un job possède :

```text
id
project_id
site_id
agent_id
plan_id
status
created_at
started_at
finished_at
requested_by
approval
workspace
input_hash
```

États autorisés :

```text
CREATED
APPROVAL_REQUIRED
QUEUED
RUNNING
PAUSED
PARTIAL
SUCCESS
FAILED
CANCELLED
BLOCKED
```

Chaque Task possède :

```text
task_id
device_id
action
status
dependencies
attempt
started_at
finished_at
exit_code
error_code
retryable
stdout_reference
stderr_reference
observed_state_reference
```

Aucune tâche ne peut être `SUCCESS` uniquement parce qu'une commande a été lancée. Elle doit avoir un résultat vérifiable.

---

# 10. Executor réel

Toutes les commandes externes passent par un `CommandExecutor` contrôlé.

Il doit gérer :

- timeout ;
- cancellation ;
- environnement contrôlé ;
- cwd contrôlé ;
- allowlist de binaires ;
- stdout ;
- stderr ;
- exit code ;
- redaction ;
- durée ;
- correlation ID.

Interdit :

```go
exec.Command("sh", "-c", userInput)
```

Préférer :

```text
binary = ansible-playbook
args   = ["-i", inventory, playbook]
```

---

# 11. Agent : fonctionnalité minimale réellement attendue

L'agent doit être un daemon persistant, pas seulement un programme qui traite un fichier et quitte.

Il doit fournir :

```text
registration
heartbeat
capabilities
local state
persistent queue
artifact cache
executor
job runner
event queue
sync
health
TUI
```

Au démarrage :

```text
load local state
load pending queue
validate configuration
start local services
connect controller
replay/recover pending tasks safely
```

Après redémarrage, une tâche ne doit pas être exécutée deux fois sans vérification d'idempotence.

---

# 12. TUI — spécification opérationnelle

La TUI doit afficher exclusivement des données provenant des use cases/agent state.

## 12.1 Header

```text
INFRAFLOW AGENT
Agent: agent-site-01
Site: agence-01
Version: 0.x
Controller: CONNECTED / DISCONNECTED
Mode: ONLINE / OFFLINE
Last sync: <timestamp réel>
```

## 12.2 Dashboard

```text
QUEUE      3
RUNNING    1
SUCCESS    18
FAILED     1
BLOCKED    2
```

Les nombres viennent du store local réel.

## 12.3 Devices

Colonnes :

```text
ID | NAME | VENDOR | MODEL | CONNECTION | STATE | LAST OBSERVED
```

États :

```text
UNKNOWN
DISCOVERED
REACHABLE
BOOTSTRAPPING
CONFIGURING
VERIFYING
HEALTHY
DRIFT
FAILED
OFFLINE
```

## 12.4 Jobs

L'utilisateur doit pouvoir sélectionner un job réel et voir :

```text
job id
status
started
elapsed
current task
progress
failed task
retry count
```

## 12.5 Logs

Afficher les logs réellement produits par le job sélectionné, avec :

```text
timestamp
level
component
job_id
task_id
device_id
message
```

Les secrets sont masqués.

## 12.6 Events

Afficher le flux réel :

```text
device.reachable
device.bootstrap_started
device.config_completed
job.failed
sync.completed
```

## 12.7 Network

La TUI doit afficher le graphe connu à partir du state réel :

```text
R1 ─── R2 ─── R3
      │
     SW1
```

Un device sans voisin observé doit apparaître comme `UNKNOWN`, pas être relié artificiellement.

## 12.8 Diagnostics

Commandes :

```text
agent status
controller status
network status
queue status
storage status
capabilities
last errors
self-test
```

Chaque diagnostic doit exécuter une vérification réelle.

---

# 13. Dashboard Web — spécification opérationnelle

Le Dashboard n'est pas une maquette. Toute carte, compteur, badge ou graphe doit être alimenté par API/backend.

## 13.1 Dashboard Overview

Widgets minimum :

```text
Agents online
Agents offline
Sites healthy
Devices healthy
Devices drifted
Jobs running
Jobs failed
Tasks queued
Events last 5 min
```

Chaque widget doit afficher sa source et son timestamp si les données peuvent être périmées.

## 13.2 Site view

Afficher :

```text
site identity
agent identity
connectivity
services
devices
networks
jobs
last synchronization
observed state age
```

## 13.3 Device view

Sections :

```text
Identity
Desired
Observed
Capabilities
Connectivity
Interfaces
Neighbors
Jobs
Logs
Events
Configuration backup
Drift
Audit
```

## 13.4 Job view

Timeline réelle :

```text
CREATED
  ↓
QUEUED
  ↓
RUNNING
  ├─ task 1 ✓
  ├─ task 2 ✓
  ├─ task 3 →
  └─ task 4 blocked
```

Afficher la raison des blocks et erreurs.

## 13.5 Topology view

Deux modes distincts :

```text
DESIRED TOPOLOGY
OBSERVED TOPOLOGY
```

Ajouter :

```text
DRIFT VIEW
```

Le frontend ne doit pas déduire un lien à partir du YAML dans le mode Observed.

## 13.6 Configuration editor

Fonctions :

```text
load actual project
edit
validate
show diagnostics
preview diff
build plan
approve/apply
```

Aucune modification n'est appliquée pendant l'édition.

## 13.7 Artifacts

Afficher réellement :

```text
artifact id
type
size
input hash
output hash
generator version
created_at
status
consumer
```

Téléchargement seulement si autorisé.

## 13.8 Audit

Filtres :

```text
user
agent
site
device
job
action
time range
result
```

---

# 14. API : aucun endpoint fictif

Chaque endpoint documenté doit exister ou être marqué `PLANNED`.

Avant d'ajouter une route, l'agent doit vérifier si elle existe déjà.

Pour chaque endpoint :

```text
request schema
response schema
auth requirement
RBAC requirement
side effect
idempotency
error codes
integration test
```

Exemple :

```text
POST /api/v1/plan/preview
side effect: NONE
requires: authenticated user
```

```text
POST /api/v1/jobs
side effect: creates job
requires: authenticated user + permission
```

```text
POST /api/v1/agents/register
side effect: registers/updates agent
requires: agent credential
```

**Un Agent ne doit pas recevoir les privilèges d'un utilisateur administrateur.**

---

# 15. Agent token vs User session

Séparation obligatoire :

```text
USER SESSION
→ projects
→ validation
→ plans
→ jobs
→ approvals
→ dashboard
→ administration

AGENT IDENTITY
→ register
→ heartbeat
→ receive authorized tasks
→ download authorized artifacts
→ report execution
→ report observed state
→ synchronize events
```

Un agent ne doit pas pouvoir créer arbitrairement un job utilisateur, modifier les permissions ou lancer une action administrative.

Tests d'autorisation obligatoires.

---

# 16. Real-time

Le Dashboard et la TUI doivent pouvoir suivre :

```text
agent connected/disconnected
job created/started/failed/completed
task state
logs
device state
topology update
sync
```

La donnée temps réel doit avoir une source et un timestamp.

Si le flux temps réel tombe, l'interface doit revenir à un polling contrôlé ou afficher `STALE`, jamais inventer la continuité.

---

# 17. DHCP/DNS/TFTP/HTTP/PXE

Ces services sont de vrais services de laboratoire lorsqu'ils sont activés.

## DHCP

Tester réellement :

```text
client discover
server offer
request
ack
lease
reservation
boot options
```

## DNS

Tester réellement :

```text
A
PTR
collision
resolution from real client
```

## TFTP

Tester réellement :

```text
real client request
file transfer
path confinement
logs
```

## HTTP/iPXE

Tester réellement :

```text
client → script
script → kernel/initrd/artifact
hash verification
```

Une génération de fichiers seule ne suffit pas pour déclarer le service `LAB-VERIFIED`.

---

# 18. Cisco — règle de support

Cisco doit être découpé par :

```text
IOS
IOS XE
IOS-XE CSR1000V
IOS L2/IOU si utilisé
```

et par méthode :

```text
SSH
CLI
NETCONF
RESTCONF
AutoInstall
ZTP
DHCP/TFTP/HTTP bootstrap
```

Un test doit préciser exactement :

```text
image
version
platform
GNS3/EVE-NG ou matériel
méthode
configuration initiale
résultat
```

Pas de `Cisco = supported` global.

---

# 19. MikroTik — règle de support

Séparer :

```text
RouterOS SSH/API
MAC access/discovery
Netinstall
Etherboot
```

Le fait qu'un port Ethernet soit `running` ou qu'une MAC soit visible ne signifie pas que le device est provisionnable.

Netinstall doit être traité comme workflow destructif potentiel et nécessiter :

```text
capability verified
model/architecture verified
package verified
approval policy
backup where possible
explicit task
```

---

# 20. FortiGate

Support uniquement selon :

```text
model
FortiOS version
management mechanism
ZTP availability
license/cloud dependency
lab result
```

Si le mécanisme réel n'est pas disponible dans le lab : `EXPERIMENTAL` ou `UNVERIFIED`, jamais `SUPPORTED`.

---

# 21. Switching et routage

Les modèles logiques doivent couvrir progressivement :

```text
VLAN
access
trunk
RSTP/STP
LACP
SVI
static routes
OSPF
BGP
VRF
QoS
```

Chaque feature doit avoir :

```text
logical model
vendor translation
verification command/API
observed state parser
```

Le parser d'état est aussi important que le générateur de configuration.

---

# 22. Proxmox

Le support Proxmox doit utiliser l'API officielle lorsqu'elle couvre le besoin.

Démonstration réelle minimale :

```text
connect
→ discover node
→ discover storage
→ discover network
→ clone/create VM
→ configure resources
→ configure network
→ start
→ wait
→ discover guest
→ verify
```

Les informations affichées dans le Dashboard doivent provenir de Proxmox ou de l'agent, pas d'une fixture.

---

# 23. Ansible réel

Deux modes doivent être distingués :

```text
GENERATE
RUN CHECK
RUN APPLY
```

`GENERATE` n'applique rien.

`CHECK` doit exécuter réellement le mode check si l'environnement le permet.

`APPLY` nécessite la policy adéquate.

Le résultat doit inclure :

```text
rc
changed
failed
stdout/stderr redacted
hosts result
```

---

# 24. Terraform réel

Séquence :

```text
generate
→ fmt
→ validate
→ init
→ plan
→ approval
→ apply
→ observe
```

`apply` est toujours une opération explicitement contrôlée.

Le state Terraform ne doit jamais apparaître dans les logs.

---

# 25. Offline-first réellement démontrable

Le mode offline n'est `LAB-VERIFIED` que si le test suivant réussit réellement :

```text
1. agent connecté
2. job accepté
3. task locale en cours
4. couper connexion controller
5. agent continue
6. task terminée
7. event persisté
8. redémarrer agent
9. event toujours présent
10. reconnecter controller
11. synchroniser
12. serveur reçoit une seule fois l'événement
13. état final cohérent
```

Une simple simulation de `controller=false` n'est pas suffisante.

---

# 26. Observed state réel

Chaque adapter doit fournir un mécanisme de lecture.

Exemples :

```text
Cisco → SSH/CLI/NETCONF/RESTCONF selon capability
MikroTik → RouterOS API/SSH
FortiGate → API/CLI
Proxmox → API
Linux → local commands/files/APIs contrôlés
```

Le système doit stocker :

```text
what
value
source
observed_at
confidence
adapter_version
```

`confidence` peut être :

```text
VERIFIED
PARTIAL
UNKNOWN
```

---

# 27. Reconciliation et drift

Le drift doit être démontrable.

Scénario :

```text
InfraFlow desired VLAN 20
        ↓
apply
        ↓
observed VLAN 20
        ↓
HEALTHY

Administrateur change réellement VLAN 20 → VLAN 30
        ↓
reconcile
        ↓
DRIFT
expected = 20
actual = 30
source = device
observed_at = timestamp
```

Puis :

```text
apply desired
→ verify
→ HEALTHY
```

---

# 28. Topology réelle

La topologie doit posséder une provenance :

```text
DESIRED
OBSERVED
INFERRED
```

`INFERRED` doit être visuellement différent et ne doit jamais être présenté comme observé.

Sources possibles :

```text
LLDP/CDP
ARP
MAC tables
routing tables
interfaces
explicit links
```

Une ligne affichée dans la topologie doit être traçable à une source.

---

# 29. Tests obligatoires

## Unit

Parser, validation, IPAM, graph, planner, scheduler, retry, state diff, templates, hashing, event model, capability resolution.

## Integration

```text
API ↔ service
service ↔ DB
provider ↔ agent
agent ↔ queue
agent ↔ executor
generator ↔ artifact store
sync ↔ controller
```

## E2E réel

Au moins un scénario sur un lab réellement disponible.

## Security

```text
auth bypass
agent privilege escalation
path traversal
command injection
secret leakage
artifact traversal
replay
duplicate events
invalid hashes
oversized input
```

## Concurrency

```bash
go test -race ./...
```

---

# 30. Critères de vérité des tests

Un test doit préciser s'il est :

```text
UNIT
INTEGRATION
SIMULATION
LAB
E2E
```

Un `UNIT` test ne peut pas justifier à lui seul `LAB-VERIFIED`.

Un `mock` ne peut pas être utilisé comme preuve d'un comportement constructeur.

Une fixture constructeur doit être étiquetée `FIXTURE`, pas `REAL DEVICE`.

---

# 31. Dashboard/TUI : interdiction des données fictives

Interdit :

```text
CPU = 23% hardcodé
RAM = 41% hardcodé
R1 = ONLINE par défaut
jobs = [demo job]
alerts = [fake alert]
links = générés uniquement pour rendre la topologie jolie
```

Autorisé :

```text
CPU = UNKNOWN
RAM = UNKNOWN
R1 = UNKNOWN
```

jusqu'à ce qu'une source réelle fournisse la donnée.

Pour une démonstration visuelle sans backend disponible, afficher explicitement :

```text
DEMO / NO LIVE DATA
```

mais ce mode ne compte pas comme démonstration fonctionnelle.

---

# 32. Health et readiness

Chaque composant doit distinguer :

```text
LIVENESS = processus vivant
READINESS = composant réellement prêt à travailler
```

Provider readiness doit vérifier ses dépendances nécessaires.

Agent readiness doit vérifier :

```text
local state
queue
required configuration
executor
controller connectivity if required
local services if enabled
```

---

# 33. Audit obligatoire

Toute action ayant un effet de bord doit générer un audit :

```text
audit_id
actor_type
actor_id
site_id
device_id
job_id
task_id
action
result
timestamp
input_hash
output_reference
```

Un audit ne doit jamais contenir un secret en clair.

---

# 34. Capability registry

Le registry doit être la seule source permettant au planner de décider qu'une capacité est utilisable.

Exemple :

```yaml
vendor: mikrotik
family: routeros
model: unknown
capabilities:
  ssh: true
  api: true
  netinstall: unknown
  ztp: unsupported
```

`unknown != true`.

Le planner doit produire :

```text
BLOCKED: capability netinstall is UNKNOWN
```

plutôt que de tenter une opération risquée.

---

# 35. Compatibilité réelle

Chaque capacité lab doit être enregistrée dans une matrice :

```text
Vendor
Family
Model
Firmware/OS
Lab platform
Connection
Provisioning method
Configuration method
Verification method
Date
Result
Evidence/reference
Status
```

La matrice devient une pièce du livrable M2.

---

# 36. Livrables obligatoires du projet

```text
01 INFRAFLOW_SPEC.md
02 README.md
03 ARCHITECTURE.md
04 diagrammes architecture
05 diagramme workflow
06 diagramme desired/observed
07 diagramme offline sync
08 API/gRPC contracts
09 YAML examples
10 compatibility matrix
11 test report
12 lab topology
13 deployment guide
14 administrator guide
15 developer guide
16 security model
17 demo scenario
18 known limitations
19 release checklist
20 changelog
```

---

# 37. Milestone DEMO-1 — à obtenir en priorité

Objectif : démontrer un pipeline réel minimal.

```text
YAML réel
 ↓
validation
 ↓
plan
 ↓
generation
 ↓
agent réel
 ↓
une opération réelle sur un lab
 ↓
verification réelle
 ↓
observed state
 ↓
Dashboard
 ↓
TUI
```

**Definition of Done :**

- [ ] aucune donnée fictive ;
- [ ] job créé par API ;
- [ ] agent réellement connecté ;
- [ ] tâche réellement exécutée ;
- [ ] sortie réellement capturée ;
- [ ] résultat réellement persisté ;
- [ ] Dashboard affiche ce résultat ;
- [ ] TUI affiche le même résultat ;
- [ ] audit enregistré.

---

# 38. Milestone DEMO-2 — réseau réel

```text
Cisco + MikroTik
       ↓
bootstrap/configuration
       ↓
verification
       ↓
topology
       ↓
drift
```

**DoD :** chaque vendor utilisé doit être identifié par modèle/version et validé dans le lab réel.

---

# 39. Milestone DEMO-3 — serveur réel

```text
PXE/iPXE
 ↓
Linux VM / bare metal disponible
 ↓
installation/bootstrap
 ↓
agent
 ↓
observed state
```

---

# 40. Milestone DEMO-4 — Proxmox

```text
InfraFlow
 ↓
Proxmox API
 ↓
VM réelle
 ↓
network
 ↓
cloud-init/PXE selon capacité
 ↓
agent
 ↓
verify
```

---

# 41. Milestone DEMO-5 — Offline

Uniquement après avoir un job réel et un agent réel :

```text
job réel
 ↓
controller coupé
 ↓
agent continue
 ↓
state local
 ↓
reconnect
 ↓
sync
```

---

# 42. Milestone DEMO-6 — Security

```text
user RBAC
agent identity
TLS/mTLS selon état réel
secret redaction
command allowlist
artifact integrity
audit
```

---

# 43. Tâches URGENTES — ordre exact

## P0 — BLOQUANT DEMO / SÉCURITÉ

- [ ] Vérifier et corriger la séparation `Agent token` / `User session`.
- [ ] Ajouter les tests HTTP d'autorisation correspondants.
- [ ] Vérifier que l'agent ne peut pas créer/retry/cancel arbitrairement les jobs utilisateur.
- [ ] Ajouter une matrice réelle `implemented/experimental/unverified`.
- [ ] Supprimer ou masquer tout widget frontend alimenté par des valeurs hardcodées.
- [ ] Ajouter `UNKNOWN/STALE/UNVERIFIED` partout où une source réelle manque.
- [ ] Ajouter CI minimale : format, vet, tests, race, build.

## P1 — BLOQUANT DU VRAI DEMO

- [ ] Choisir **un seul premier workflow réellement disponible** dans le lab.
- [ ] Le faire fonctionner de `infra.yaml` jusqu'à l'équipement réel.
- [ ] Implémenter/terminer `ExecutionJob`.
- [ ] Implémenter `CommandExecutor` sécurisé.
- [ ] Capturer stdout/stderr/exit code/durée.
- [ ] Ajouter timeout/cancellation.
- [ ] Ajouter vérification réelle après apply.
- [ ] Persister observed state.
- [ ] Afficher le résultat dans TUI.
- [ ] Afficher le même résultat dans Dashboard.

## P2 — APRÈS LE PREMIER DEMO

- [ ] Deuxième vendor réel.
- [ ] Switching réel.
- [ ] Topologie observée.
- [ ] Drift réel.
- [ ] DHCP/DNS/TFTP/PXE réel selon matériel disponible.
- [ ] Proxmox réel.
- [ ] Offline queue réelle.
- [ ] Reconnexion/synchronisation réelle.

## P3 — HARDENING

- [ ] mTLS.
- [ ] Secret store abstraction + backend réel si nécessaire.
- [ ] policy engine.
- [ ] backups.
- [ ] rollback uniquement lorsqu'il est réellement supporté.
- [ ] fuzzing.
- [ ] security scan.
- [ ] performance/memory profiling.

---

# 44. Tâches interdites tant que P0/P1 ne sont pas terminées

Les agents ne doivent PAS partir sur :

```text
nouveau redesign frontend
nouvelle architecture
nouveau framework
support de 10 nouveaux vendors
cloud multi-provider
HA complexe
Terraform provider custom
feature non nécessaire à la démonstration
```

si le premier pipeline réel n'est pas fonctionnel.

**Freeze architecture jusqu'à obtention de DEMO-1.**

---

# 45. Checklist finale avant démonstration encadreur

```text
[ ] repository propre
[ ] branche develop compile
[ ] tests passent
[ ] race tests passent si possible
[ ] Provider démarre
[ ] Agent démarre
[ ] TUI démarre
[ ] Dashboard démarre
[ ] aucun faux device
[ ] aucun faux job
[ ] aucun faux compteur
[ ] source des données identifiable
[ ] YAML réel chargé
[ ] validation réelle
[ ] plan réel
[ ] job réel
[ ] agent réel
[ ] exécution réelle
[ ] résultat réel
[ ] verification réelle
[ ] observed state
[ ] topology réelle ou explicitement UNKNOWN
[ ] audit réel
[ ] erreurs visibles
[ ] retry réel
[ ] capability matrix à jour
[ ] limites documentées
```

---

# 46. Règle finale pour les agents

> **Ne jamais écrire « terminé » parce que le code compile.**
>
> Une feature est terminée uniquement lorsqu'elle est observable, testée et documentée.
>
> **Ne jamais écrire « supporté » parce que la documentation constructeur décrit la fonctionnalité.**
>
> Elle est supportée uniquement après validation adaptée au modèle/version/méthode.
>
> **Ne jamais remplir une interface avec des données fictives pour donner l'impression que la fonctionnalité fonctionne.**
>
> Afficher `UNKNOWN`, `NO DATA`, `STALE`, `EXPERIMENTAL` ou `UNVERIFIED`.
>
> **Ne jamais inventer une commande, une API, un comportement constructeur ou un résultat de laboratoire.**
>
> Si l'information n'est pas vérifiée : `UNKNOWN`.

---

# 47. Objectif final mesurable

InfraFlow est considéré comme ayant atteint son objectif principal lorsque l'équipe peut, avec l'environnement réellement disponible :

```text
décrire
  ↓
valider
  ↓
planifier
  ↓
générer
  ↓
provisionner réellement
  ↓
observer réellement
  ↓
vérifier réellement
  ↓
détecter le drift
  ↓
réconcilier
  ↓
visualiser dans Web + TUI
  ↓
auditer
  ↓
continuer localement lors d'une coupure
  ↓
synchroniser après reconnexion
```

Aucun maillon ne doit être considéré comme acquis sans preuve.

---

# 48. État de référence au 06/10/2026

Cette section doit être mise à jour par les agents après chaque milestone. Elle sert à empêcher toute hallucination sur l'état du dépôt.

```text
Repository: Nouments/infraflow
Branch: develop

Architecture Provider/Agent: PRESENT
YAML validation: PRESENT
Planning: PRESENT
Scheduler: PRESENT
Artifact generation: PRESENT
Ansible generation: PRESENT
Terraform generation/validation: PRESENT
DHCP/TFTP/bootstrap components: PRESENT / certains EXPERIMENTAL
Web infrastructure editor: PRESENT
Plan preview: PRESENT
Auth/RBAC/session: PRESENT
Audit: PRESENT
Reconciliation primitives: PRESENT

REAL DEVICE PROVISIONING:
→ vérifier dans le dépôt + lab avant de déclarer LAB-VERIFIED

OFFLINE CONTINUITY:
→ ne pas déclarer LAB-VERIFIED avant test de coupure réel

OBSERVED TOPOLOGY:
→ ne pas déclarer complète avant source réelle LLDP/CDP/ARP/MAC/routing

TUI:
→ doit afficher les états réels de l'agent, pas des données de démonstration

WEB DASHBOARD:
→ doit afficher les données backend réelles, pas des données hardcodées
```

Cette section est volontairement prudente : les agents doivent la remplacer par des preuves concrètes après exécution des tests.

---

# 49. Commande de travail standard pour les agents

Chaque agent doit recevoir une tâche sous cette forme :

```text
TASK:
<fonction précise>

CONSTRAINTS:
- no fake data
- no invented vendor API
- reuse existing architecture
- inspect current implementation first
- preserve offline design
- add tests

DONE WHEN:
- implementation
- unit tests
- integration test if applicable
- real lab test if vendor feature
- logs/result observable
- TUI/dashboard source wired
- documentation
- capability/status updated

REPORT:
- implemented
- verified
- not implemented
- commands/tests run
- real resources used
- limitations
- files changed
- next task
```

---

# 50. Priorité absolue

**Le projet ne doit plus chercher à paraître complet. Il doit devenir réellement complet, morceau par morceau, avec preuve.**

Le meilleur démonstrateur est un petit workflow entièrement réel :

```text
YAML
 → Plan
 → Job
 → Agent
 → Device réel
 → Configuration réelle
 → Verification réelle
 → Observed State
 → Dashboard
 → TUI
 → Audit
```

Une fois ce chemin fiable, les vendors, PXE, Proxmox, offline, multi-site et autres capacités peuvent être ajoutés sans changer le cœur de l'architecture.

# InfraFlow — Cahier des charges technique complet

> **Document de référence pour le développement avec Codex et les agents
> de développement.**
>
> Ce document décrit le produit, l’architecture, les contrats, les
> composants, les workflows, les templates, les mécanismes de
> provisioning, les tests, la sécurité, la roadmap et les critères
> d’acceptation.
>
> **Règle fondamentale : aucune capacité fournisseur ne doit être
> inventée.** Une fonctionnalité de provisioning ne peut être déclarée
> supportée que si elle possède une documentation de référence, un
> adapter isolé, des fixtures ou un environnement de laboratoire, des
> tests automatisés et un résultat observable.

------------------------------------------------------------------------

## 1. Vision du projet

InfraFlow est une plateforme légère d’automatisation et d’orchestration
d’infrastructures permettant de décrire une infrastructure de manière
déclarative, de générer les artefacts nécessaires, de provisionner les
équipements et serveurs, de suivre l’état du déploiement en temps réel
et de continuer à fonctionner localement lorsqu’un site perd sa
connexion avec le cloud.

InfraFlow doit couvrir progressivement :

- réseaux multi-constructeurs ;
- routeurs ;
- switches ;
- firewalls ;
- serveurs bare metal ;
- Proxmox ;
- machines virtuelles ;
- postes et équipements finaux ;
- provisioning PXE/iPXE ;
- DHCP ;
- DNS ;
- TFTP lorsque requis par un workflow ;
- transfert HTTP/HTTPS ;
- transfert de fichiers contrôlé ;
- Ansible ;
- Terraform ;
- VPN/Tailscale ;
- cloud providers ;
- inventaire matériel ;
- découverte par MAC ;
- topologie ;
- état temps réel ;
- reprise après coupure ;
- mode multisite ;
- mode local-only ;
- sécurité ;
- audit.

L’objectif n’est pas de créer un énorme monolithe qui implémente
directement chaque constructeur.

L’objectif est de construire un **moteur d’orchestration générique**
avec des **adapters spécialisés**, des contrats stables et des capacités
explicitement déclarées.

------------------------------------------------------------------------

# 2. Objectifs fonctionnels

## 2.1 Objectifs principaux

InfraFlow doit permettre :

- [ ] créer une infrastructure à partir d’un fichier YAML déclaratif ;
- [ ] valider le YAML avant toute action ;
- [ ] détecter les erreurs de schéma avant provisioning ;
- [ ] calculer un plan de déploiement ;
- [ ] afficher ce plan avant exécution ;
- [ ] générer les fichiers intermédiaires ;
- [ ] générer les configurations Ansible ;
- [ ] générer les modules/templates Terraform nécessaires ;
- [ ] générer les fichiers DHCP ;
- [ ] générer les fichiers DNS ;
- [ ] générer les fichiers PXE/iPXE ;
- [ ] générer les fichiers de provisioning constructeur ;
- [ ] générer les scripts de bootstrap ;
- [ ] générer les inventaires ;
- [ ] exécuter le provisioning ;
- [ ] suivre chaque étape ;
- [ ] journaliser chaque opération ;
- [ ] conserver les résultats ;
- [ ] gérer les retries ;
- [ ] gérer les dépendances ;
- [ ] gérer plusieurs sites ;
- [ ] continuer localement hors connexion ;
- [ ] synchroniser l’état lorsque la connexion revient ;
- [ ] produire une topologie ;
- [ ] fournir une TUI pour l’agent ;
- [ ] fournir une API/backend ;
- [ ] fournir une interface web ;
- [ ] exposer des événements temps réel ;
- [ ] intégrer des tests unitaires, intégration et end-to-end ;
- [ ] permettre à plusieurs agents de travailler parallèlement sans
  modifier anarchiquement les mêmes fichiers.

------------------------------------------------------------------------

# 3. Principes non négociables

## 3.1 Declarative first

Le YAML décrit **l’état désiré**, pas une suite de commandes
impératives.

Exemple :

``` yaml
sites:
  - name: agence-01

    bootstrap:
      network: 192.168.100.0/24

    devices:
      - name: R1
        role: router
        vendor: cisco
        model: csr1000v

      - name: R2
        role: router
        vendor: mikrotik
        model: routeros

      - name: FW1
        role: firewall
        vendor: fortinet
        model: fortigate

      - name: SW1
        role: switch
        vendor: cisco

    links:
      - a: R1:Gi1
        b: R2:ether1
        network: 10.0.0.0/30
```

InfraFlow doit transformer ce modèle en plan d’exécution.

------------------------------------------------------------------------

## 3.2 Desired state vs observed state

Deux états doivent toujours être distingués :

### Desired state

Ce que le YAML demande.

### Observed state

Ce que l’agent a réellement observé.

Ne jamais remplacer silencieusement l’état désiré par l’état observé.

Exemple :

``` text
desired:
  hostname: R2
  management_ip: 10.10.0.2

observed:
  hostname: R2
  management_ip: 10.10.0.3

status:
  drift: true
```

------------------------------------------------------------------------

## 3.3 Aucun effet de bord pendant la validation

La commande de validation ne doit :

- ni modifier un équipement ;
- ni supprimer une ressource ;
- ni démarrer un provisioning ;
- ni exécuter un playbook ;
- ni lancer `terraform apply`.

Elle doit uniquement :

- parser ;
- valider ;
- normaliser ;
- détecter les contradictions ;
- produire des diagnostics.

------------------------------------------------------------------------

## 3.4 Plan avant Apply

InfraFlow doit séparer :

``` text
parse
  ↓
validate
  ↓
normalize
  ↓
resolve dependencies
  ↓
generate plan
  ↓
review
  ↓
apply
  ↓
observe
  ↓
reconcile
```

------------------------------------------------------------------------

## 3.5 Les adapters sont responsables des particularités fournisseurs

Le core ne doit jamais contenir :

``` go
if vendor == "cisco" {
    ...
}
```

partout dans le code.

Préférer :

``` go
type Provisioner interface {
    Capabilities(ctx context.Context) (Capabilities, error)
    Bootstrap(ctx context.Context, req BootstrapRequest) (Result, error)
    Configure(ctx context.Context, req ConfigureRequest) (Result, error)
    Verify(ctx context.Context, req VerifyRequest) (VerificationResult, error)
}
```

Puis :

``` text
core
 ├── cisco adapter
 ├── mikrotik adapter
 ├── fortinet adapter
 ├── proxmox adapter
 ├── linux adapter
 └── cloud adapters
```

------------------------------------------------------------------------

# 4. Architecture globale

## 4.1 Architecture logique

``` text
                         ┌─────────────────────────┐
                         │       Web Frontend      │
                         │ topology / jobs / state │
                         └────────────┬────────────┘
                                      │ HTTPS/WSS
                         ┌────────────▼────────────┐
                         │       Backend API       │
                         │ auth / projects / jobs  │
                         │ state / events / audit  │
                         └────────────┬────────────┘
                                      │
                         ┌────────────▼────────────┐
                         │     Orchestrator        │
                         │ planner / scheduler     │
                         │ DAG / BFS / DFS / retry │
                         └──────┬─────────┬────────┘
                                │         │
                     ┌──────────▼───┐ ┌──▼──────────┐
                     │ Generator    │ │ State Store │
                     │ templates    │ │ desired /   │
                     │ Ansible      │ │ observed    │
                     │ Terraform    │ │ audit       │
                     │ DHCP/PXE     │ └─────────────┘
                     └──────┬───────┘
                            │
                     ┌──────▼───────────────────┐
                     │ Provisioning adapters    │
                     │ Cisco / MikroTik / Forti │
                     │ Proxmox / Linux / Cloud  │
                     └───────────┬──────────────┘
                                 │
                       site connection / agent
                                 │
                  ┌──────────────▼──────────────┐
                  │          Site Agent         │
                  │ local orchestrator          │
                  │ local state                 │
                  │ local services              │
                  │ TUI                         │
                  │ DHCP/DNS/file service       │
                  │ Terraform/Ansible runner    │
                  └──────────────┬──────────────┘
                                 │
                 ┌───────────────▼────────────────┐
                 │       Site infrastructure      │
                 │ routers / switches / firewalls │
                 │ servers / Proxmox / PCs        │
                 └────────────────────────────────┘
```

------------------------------------------------------------------------

# 5. Déploiement cloud / on-premise / local

InfraFlow doit supporter trois modes.

## 5.1 Mode cloud

``` text
Cloud controller
      |
      +--- Site Agent A
      +--- Site Agent B
      +--- Site Agent C
```

Le cloud :

- reçoit les déclarations ;
- génère les plans ;
- suit les sites ;
- distribue les jobs ;
- reçoit les états ;
- affiche la topologie globale.

------------------------------------------------------------------------

## 5.2 Mode on-premise

Le provisioner peut être installé dans l’infrastructure locale.

``` text
LAN
 |
 +--- InfraFlow Controller
 |
 +--- Agent
 |
 +--- Devices
```

Ce mode doit fonctionner sans dépendance obligatoire à Internet.

------------------------------------------------------------------------

## 5.3 Mode local-only

Le site doit continuer à fonctionner même si le lien cloud tombe.

``` text
             INTERNET
                 X
                 |
        ┌────────▼────────┐
        │   Site Agent    │
        │ local state     │
        │ local queue     │
        │ local executor  │
        └───────┬────────┘
                |
        infrastructure
```

Pendant la coupure :

- les jobs déjà acceptés continuent ;
- l’agent peut orchestrer les opérations locales ;
- les états sont stockés localement ;
- les événements sont mis en queue ;
- les résultats sont signés/hashés ;
- aucune opération dépendant obligatoirement du cloud ne doit être
  exécutée comme si elle était disponible ;
- à la reconnexion, l’agent synchronise son état.

------------------------------------------------------------------------

# 6. Modèle de synchronisation offline

Chaque événement doit posséder un identifiant stable :

``` text
event_id
site_id
agent_id
job_id
sequence
timestamp
type
payload
hash
previous_hash
```

Exemple :

``` json
{
  "event_id": "evt-01",
  "site_id": "site-01",
  "agent_id": "agent-site-01",
  "sequence": 182,
  "type": "device.provisioned",
  "job_id": "job-42"
}
```

La synchronisation doit être idempotente.

Un événement reçu deux fois ne doit pas provoquer deux déploiements.

------------------------------------------------------------------------

# 7. Architecture Clean

## 7.1 Couches

``` text
internal/
├── domain/
├── application/
├── ports/
├── adapters/
├── infrastructure/
└── delivery/
```

### domain

Contient :

- Device ;
- Site ;
- Link ;
- Network ;
- Job ;
- Task ;
- Plan ;
- Capability ;
- DesiredState ;
- ObservedState ;
- Event ;
- Result.

Le domaine ne doit dépendre :

- ni de HTTP ;
- ni de SQL ;
- ni de Ansible ;
- ni de Terraform ;
- ni de SSH ;
- ni d’un constructeur.

------------------------------------------------------------------------

## 7.2 application

Contient les use cases :

``` text
CreateProject
ValidateInfrastructure
BuildPlan
GenerateArtifacts
StartDeployment
PauseDeployment
ResumeDeployment
RetryJob
GetStatus
ReconcileState
SyncAgent
RegisterAgent
```

------------------------------------------------------------------------

## 7.3 ports

Exemples :

``` go
type DeviceRepository interface {}

type JobRepository interface {}

type EventStore interface {}

type SecretStore interface {}

type Provisioner interface {}

type FileService interface {}

type DHCPService interface {}

type DNSService interface {}

type Executor interface {}

type ArtifactGenerator interface {}
```

------------------------------------------------------------------------

## 7.4 adapters

``` text
adapters/
├── cisco/
├── mikrotik/
├── fortinet/
├── proxmox/
├── linux/
├── terraform/
├── ansible/
├── dhcp/
├── dns/
├── pxe/
├── ipxe/
├── ssh/
├── http/
└── cloud/
```

------------------------------------------------------------------------

# 8. Structure de repository proposée

``` text
infraflow/
├── cmd/
│   ├── infraflow-server/
│   ├── infraflow-agent/
│   ├── infraflow/
│   └── infraflow-provider/
│
├── internal/
│   ├── domain/
│   ├── application/
│   ├── ports/
│   ├── adapters/
│   ├── orchestrator/
│   ├── scheduler/
│   ├── planner/
│   ├── generator/
│   ├── state/
│   ├── events/
│   ├── security/
│   ├── inventory/
│   ├── topology/
│   ├── sync/
│   ├── agent/
│   ├── tui/
│   └── api/
│
├── pkg/
│   └── sdk/
│
├── api/
│   ├── openapi/
│   └── proto/
│
├── templates/
│   ├── ansible/
│   ├── terraform/
│   ├── dhcp/
│   ├── dns/
│   ├── pxe/
│   ├── ipxe/
│   ├── cisco/
│   ├── mikrotik/
│   ├── fortinet/
│   ├── proxmox/
│   └── linux/
│
├── schemas/
│   ├── infra.schema.json
│   └── capability.schema.json
│
├── fixtures/
│   ├── cisco/
│   ├── mikrotik/
│   ├── fortinet/
│   ├── proxmox/
│   └── pxe/
│
├── examples/
├── docs/
├── tests/
│   ├── unit/
│   ├── integration/
│   ├── e2e/
│   └── fixtures/
│
├── scripts/
├── deployments/
├── Makefile
├── go.mod
├── go.sum
├── README.md
├── ARCHITECTURE.md
└── INFRAFLOW_SPEC.md
```

------------------------------------------------------------------------

# 9. Modèle de données

## 9.1 Site

``` yaml
site:
  id: site-01
  name: agence-01
  mode: local-capable

  bootstrap:
    network: 192.168.100.0/24
    gateway: 192.168.100.1

  services:
    dhcp: true
    dns: true
    tftp: true
    http: true
    ipxe: true
```

------------------------------------------------------------------------

## 9.2 Device

``` yaml
device:
  id: device-r1
  name: R1
  vendor: cisco
  family: router
  model: csr1000v
  role: edge-router

  identity:
    macs:
      - "00:11:22:33:44:55"

  management:
    ipv4: 10.0.0.1

  provisioning:
    method: cisco-ztp
```

Le modèle ne doit pas supposer que tous les équipements supportent le
même mécanisme.

------------------------------------------------------------------------

# 10. Matrice de capacités

Chaque adapter doit publier ses capacités.

Exemple :

``` yaml
vendor: cisco
model_family: ios-xe

capabilities:
  bootstrap:
    dhcp: true
    tftp: true
    http: true
    ztp: true
    autoinstall: true

  configuration:
    ssh: true
    netconf: unknown
    restconf: unknown

  verification:
    interfaces: true
    routing: true
    version: true
```

Valeurs autorisées :

``` text
true
false
unknown
unsupported
```

`unknown` ne doit jamais être interprété comme `true`.

------------------------------------------------------------------------

# 11. Mécanisme anti-hallucination technique

Toute fonctionnalité fournisseur doit avoir :

``` text
Capability
  ↓
Reference
  ↓
Adapter
  ↓
Fixture
  ↓
Unit tests
  ↓
Integration test
  ↓
Lab validation
  ↓
Supported
```

Une feature reste `experimental` tant que les étapes de validation ne
sont pas terminées.

------------------------------------------------------------------------

# 12. Inventory et identification

InfraFlow doit pouvoir identifier un équipement à partir de :

1.  MAC ;
2.  serial ;
3.  hostname ;
4.  management IP ;
5.  vendor ;
6.  model ;
7.  device ID préenregistré ;
8.  fingerprint observé.

Priorité recommandée :

``` text
serial exact
↓
MAC exact
↓
device identity enregistrée
↓
management IP
↓
fingerprint
↓
unknown device
```

Un équipement inconnu ne doit pas être automatiquement déployé en
production.

------------------------------------------------------------------------

# 13. Bootstrap network

Le réseau bootstrap sert uniquement à établir une première
communication.

Exemple :

``` text
192.168.100.0/24

192.168.100.1   agent/provisioner
192.168.100.10  R1
192.168.100.11  R2
192.168.100.12  SW1
192.168.100.13  FW1
```

Le bootstrap doit être temporaire lorsque cela est possible.

------------------------------------------------------------------------

# 14. DHCP

Le service DHCP InfraFlow doit supporter :

- pools ;
- reservations ;
- MAC ;
- client identifier ;
- hostname ;
- gateway ;
- DNS ;
- boot filename ;
- next server ;
- options spécifiques ;
- vendor-class ;
- user-class ;
- règles conditionnelles ;
- expiration ;
- audit des leases.

Exemple abstrait :

``` yaml
dhcp:
  networks:
    - name: bootstrap
      subnet: 192.168.100.0/24
      range:
        start: 192.168.100.100
        end: 192.168.100.200

      reservations:
        - mac: "00:11:22:33:44:55"
          ip: 192.168.100.10
          hostname: R1

      boot:
        mode: ipxe
        server: 192.168.100.1
```

Ne jamais supposer qu’un équipement utilisera automatiquement les mêmes
options DHCP qu’un autre constructeur.

------------------------------------------------------------------------

# 15. DNS

Le DNS local doit pouvoir fournir :

``` text
r1.site-01.infraflow.local
r2.site-01.infraflow.local
sw1.site-01.infraflow.local
agent.site-01.infraflow.local
provisioner.site-01.infraflow.local
```

Fonctions :

- A ;
- AAAA ;
- PTR ;
- éventuellement CNAME ;
- zones locales ;
- reverse zones ;
- TTL ;
- validation des collisions ;
- génération déterministe.

------------------------------------------------------------------------

# 16. TFTP

TFTP doit être considéré comme un protocole de bootstrap legacy ou
spécifique, pas comme le transport général par défaut.

Utilisation :

``` text
Cisco AutoInstall
certain workflows PXE
certain bootstraps legacy
```

Pour les fichiers volumineux ou les scripts modernes, préférer lorsque
le matériel le permet :

``` text
HTTP/HTTPS
```

Le serveur TFTP doit être :

- isolé ;
- read-only par défaut ;
- limité au répertoire d’artefacts ;
- journalisé ;
- incapable d’accéder au filesystem arbitraire.

------------------------------------------------------------------------

# 17. PXE / iPXE

Architecture :

``` text
DHCP
  |
  +-- firmware/PXE client
          |
          v
      bootstrap loader
          |
          v
         iPXE
          |
          v
      HTTP/HTTPS
          |
          v
      iPXE script
          |
          v
       installer
```

InfraFlow doit générer :

``` text
pxe/
├── boot/
├── grub/
├── ipxe/
├── scripts/
├── images/
└── metadata/
```

Exemple de script abstrait :

``` ipxe
#!ipxe

set server http://192.168.100.1
kernel ${server}/images/kernel
initrd ${server}/images/initrd
imgargs kernel initrd=initrd
boot
```

Le script réel doit être généré depuis un template et validé avant
publication.

------------------------------------------------------------------------

# 18. Génération de fichiers

Le générateur doit être déterministe.

Pour un même :

``` text
infra.yaml
template version
generator version
```

il doit produire le même résultat.

Chaque artefact doit posséder :

``` text
artifact_id
type
path
generator_version
template_version
input_hash
output_hash
created_at
```

------------------------------------------------------------------------

# 19. Moteur de templates

Le moteur doit séparer :

``` text
template
+
normalized model
+
vendor/model context
+
variables
=
artifact