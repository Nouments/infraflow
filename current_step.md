# STEP 01.5 — Execution Plan

## Objectif

Implémenter uniquement la représentation et la construction d'un **Execution Plan déterministe** à partir de l'état désiré validé.

Cette étape prépare InfraFlow à l'exécution réelle future.

Elle ne doit exécuter aucune commande et ne doit contacter aucun équipement.

La distinction fondamentale doit rester :

```text
DESIRED
    ↓
PLANNED
    ↓
GENERATED
    ↓
EXECUTED
    ↓
VERIFIED
```

et :

```text
GENERATED != EXECUTED
```

---

# 1. Principe

InfraFlow doit pouvoir transformer un état désiré validé en un plan explicite décrivant :

* quelles actions doivent être effectuées ;
* sur quel device ;
* dans quel ordre ;
* avec quel provider/méthode ;
* quels artefacts générés sont associés ;
* quelles dépendances existent entre actions.

Le plan doit être **descriptif uniquement**.

Il ne doit jamais exécuter lui-même les actions.

---

# 2. ExecutionPlan

Introduire un modèle de domaine clair, par exemple :

```go
type ExecutionPlan struct {
    ID        string         `json:"id"`
    Site      string         `json:"site"`
    Status    string         `json:"status"`
    Steps     []ExecutionStep `json:"steps"`
}
```

et :

```go
type ExecutionStep struct {
    ID          string   `json:"id"`
    Device      string   `json:"device"`
    Action      string   `json:"action"`
    Method      string   `json:"method"`
    Dependencies []string `json:"dependencies,omitempty"`
    Artifacts   []string `json:"artifacts,omitempty"`
}
```

Adapter les noms/types à l'architecture existante si un modèle équivalent existe déjà.

**Ne pas créer un deuxième système parallèle si le repository possède déjà un modèle approprié.**

---

# 3. Statuts du plan

Le plan doit utiliser un statut explicite.

Minimum :

```text
PLANNED
EXECUTING
EXECUTED
FAILED
VERIFIED
```

Mais cette étape ne doit réellement utiliser que :

```text
PLANNED
```

pour un plan nouvellement créé.

Ne jamais créer artificiellement :

```text
EXECUTED
VERIFIED
```

lors de la construction du plan.

---

# 4. Génération déterministe

À partir du même état désiré, la construction du plan doit produire le même résultat logique.

Exemple :

```text
Device R1
    interface Gi1
    interface Gi2

Device R2
    interface Gi1
```

doit produire des steps déterministes :

```text
R1 / configure_interfaces
R2 / configure_interfaces
```

L'ordre doit être stable.

Ne pas dépendre de l'ordre aléatoire d'une map Go.

Utiliser un tri déterministe lorsque nécessaire.

---

# 5. Actions

Cette étape ne doit introduire qu'un vocabulaire minimal d'actions.

Exemple :

```text
configure_interfaces
```

Une action doit décrire une intention.

Elle ne doit pas contenir directement :

```text
ssh command
telnet command
netconf RPC
REST API request
shell command
```

Ces mécanismes appartiendront à l'étape d'exécution future.

---

# 6. Méthode / Provider

Le plan doit conserver la méthode nécessaire à l'exécution future.

Exemples :

```text
cisco / iosxe / netconf
mikrotik / routeros / api
fortinet / fortios / https
```

Mais attention :

Le fait qu'une méthode soit présente dans le plan ne signifie PAS qu'elle est :

```text
SUPPORTED
IMPLEMENTED
LAB-VERIFIED
```

Ne pas transformer automatiquement :

```text
method = netconf
```

en :

```text
verified = true
```

Utiliser les informations existantes du capability registry/vendor profile si elles existent déjà.

Ne pas créer un nouveau capability registry.

---

# 7. Dépendances

Le modèle doit permettre :

```text
step B depends_on step A
```

Exemple :

```text
bootstrap
   ↓
management
   ↓
network configuration
```

Mais cette étape ne doit pas encore implémenter un moteur complexe de DAG.

Il suffit de représenter les dépendances explicitement et de produire un ordre déterministe.

Ne pas implémenter :

* BFS réel de déploiement ;
* DFS réel ;
* retry ;
* parallélisation ;
* scheduling ;
* worker pool.

Ces éléments appartiendront à une future étape.

---

# 8. Relation avec LifecycleState

Lorsqu'un ExecutionPlan est construit avec succès :

```text
Desired = true
Planned = true
Generated = false
Executed = false
Verified = false
Observed = false
```

La construction du plan doit pouvoir positionner :

```text
PLANNED
```

mais elle ne doit jamais positionner :

```text
GENERATED
EXECUTED
VERIFIED
OBSERVED
```

automatiquement.

Si l'architecture possède déjà une fonction de transition, utiliser :

```go
state.Transition(StatePlanned)
```

plutôt que de modifier directement les booléens.

---

# 9. Aucun fake data

Interdiction stricte :

```text
mock device
fake execution
fake command output
fake success
fake metrics
fake observation
fake verification
```

Les steps doivent provenir exclusivement de l'état désiré réellement fourni au planner.

Si aucune configuration réseau n'existe :

```text
steps = []
```

ou le comportement déjà prévu par l'architecture.

Ne pas inventer une action.

---

# 10. Génération d'artefacts

Le planner peut référencer les artefacts déjà générés, mais ne doit pas prétendre qu'ils ont été exécutés.

Exemple :

```text
Artifact:
site/ansible/vendor-playbook.yml
```

peut être référencé comme :

```text
Artifacts:
    - site/ansible/vendor-playbook.yml
```

Cela signifie uniquement :

```text
artifact generated
```

et jamais :

```text
device configured
```

---

# 11. Intégration avec la génération existante

Inspecter d'abord l'architecture actuelle.

Ne pas réécrire :

```text
generation
vendor_ansible
Cisco renderer
MikroTik renderer
FortiGate renderer
```

Le planner doit consommer leurs résultats ou les modèles existants lorsque cela est approprié.

Ne pas déplacer inutilement du code.

---

# 12. Tests obligatoires

Ajouter des tests unitaires.

## Test 1 — plan vide

Un site sans configuration réseau ne doit pas produire d'action inventée.

Vérifier :

```text
steps = 0
```

---

## Test 2 — plan Cisco

Avec un device Cisco possédant deux interfaces :

```text
R1
Gi1
Gi2
```

le planner doit produire une action correspondant à la configuration des interfaces.

Vérifier :

```text
Device = R1
Action = configure_interfaces
```

---

## Test 3 — déterminisme

Construire deux fois le plan à partir du même état.

Vérifier que les résultats sont identiques.

---

## Test 4 — plusieurs devices

Avec :

```text
R1
R2
R3
```

vérifier que l'ordre des steps est déterministe.

Ne jamais dépendre de l'ordre d'une map.

---

## Test 5 — lifecycle

Après construction du plan :

```text
Desired   = true
Planned   = true
Generated = false
Executed  = false
Verified  = false
Observed  = false
```

---

## Test 6 — plan != execution

Construire un plan ne doit jamais :

```text
Executed = true
```

et ne doit lancer aucune commande externe.

---

## Test 7 — plan != verification

Construire un plan ne doit jamais :

```text
Verified = true
```

---

## Test 8 — inferred != observed

Si une information utilisée par le planner est `INFERRED`, elle ne doit jamais devenir :

```text
Observed = true
```

---

# 13. Sécurité

Le planner ne doit jamais exécuter :

```text
exec.Command
os/exec
ssh
telnet
netconf
HTTP mutation
REST API mutation
```

Aucune connexion réseau ne doit être ouverte.

Le planner est une étape pure :

```text
Input
  ↓
Validation
  ↓
Planning
  ↓
ExecutionPlan
```

---

# 14. Ce qui est explicitement interdit dans ce step

NE PAS implémenter :

```text
SSH
NETCONF execution
RESTCONF
REST API execution
Telnet
GNS3
ZTP
PXE
iPXE
DHCP
TFTP
FTP
Proxmox
real execution
retry
BFS deployment
DFS deployment
parallel deployment
worker pool
drift
reconciliation
Web
TUI
agent
API
```

Ne pas modifier le comportement des providers pour exécuter réellement les actions.

---

# 15. Recherche préalable obligatoire

Avant de coder :

```bash
grep -R "type .*Plan" -n internal pkg
grep -R "PLANNED" -n internal pkg
grep -R "Transition(" -n internal pkg
grep -R "GenerateAnsible" -n internal pkg
grep -R "Artifact" -n internal pkg
```

Identifier les modèles et fonctions existants.

Réutiliser l'architecture existante lorsqu'elle couvre déjà le besoin.

---

# 16. Validation obligatoire

Exécuter :

```bash
gofmt -w <fichiers-modifiés>

go test ./... -count=1

git diff --check

git status --short
```

Puis inspecter :

```bash
git diff
```

Vérifier particulièrement qu'aucune exécution réelle n'a été introduite.

Recherche de sécurité :

```bash
grep -R "exec.Command\|os/exec\|ssh\|netconf\|restconf\|http.NewRequest" -n <fichiers-modifiés>
```

Aucune nouvelle exécution externe ne doit apparaître dans le planner.

---

# 17. Rapport final obligatoire

Répondre uniquement :

```text
Fichiers modifiés:
Modèle ExecutionPlan réutilisé ou créé:
Planner ajouté:
Actions supportées:
Déterminisme:
Lifecycle PLANNED:
Execution réelle introduite: oui/non
Tests ajoutés/modifiés:
Résultat des tests:
Commit:
```

Ne rien implémenter au-delà de ce Step 01.5.
