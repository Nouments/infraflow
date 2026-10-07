# STEP 01.5 — EXECUTION PLAN DÉTERMINISTE

## 0. CONTEXTE OBLIGATOIRE

Le repository possède déjà une première implémentation de planning :

```text
internal/domain/plan.go
internal/application/planner/planner.go
internal/application/planner/planner_test.go
```

Le modèle actuel contient notamment :

```go
type Plan struct {
    Version string
    Status  string
    Tasks   []Task
}

type Task struct {
    ID
    Site
    Target
    Action
    Status
    Reason
    Dependencies
}
```

Le planner actuel ajoute notamment :

```text
generate_inventory
generate_topology
provision_device
```

Cette implémentation ne correspond pas encore complètement au contrat du Step 01.5.

### RÈGLE ABSOLUE

**NE PAS créer un deuxième système de planning parallèle.**

Avant toute modification :

```bash
grep -R "type Plan" -n internal pkg
grep -R "type Task" -n internal pkg
grep -R "Build(" -n internal/application internal
grep -R "PLANNED" -n internal pkg
grep -R "Transition(" -n internal pkg
grep -R "CapabilityRegistry" -n internal pkg
grep -R "TemplateRegistry" -n internal pkg
```

Comprendre les dépendances existantes avant de modifier quoi que ce soit.

---

# 1. OBJECTIF EXACT

Transformer l'implémentation actuelle en un véritable **Execution Plan déterministe et descriptif**.

Le résultat doit représenter :

```text
DESIRED
   ↓
PLANNED
```

et préparer les futures étapes :

```text
PLANNED
   ↓
GENERATED
   ↓
EXECUTED
   ↓
VERIFIED
```

Mais dans ce Step :

```text
PLANNED = OUI
GENERATED = NON
EXECUTED = NON
VERIFIED = NON
OBSERVED = NON
```

Le planner :

* analyse l'état désiré ;
* construit un plan ;
* ordonne les actions ;
* représente les dépendances ;
* représente la méthode/provider future lorsque celle-ci est explicitement connue ;
* peut référencer des artefacts déjà générés ;
* ne contacte aucun équipement ;
* ne lance aucune commande ;
* ne vérifie aucun équipement.

---

# 2. CONTRAINTE ARCHITECTURALE MAJEURE

Il existe déjà :

```text
domain.Plan
domain.Task
planner.Build()
```

Il faut décider, après inspection, si ces modèles peuvent être adaptés.

### Préférence

Réutiliser :

```go
Plan
Task
```

si cela permet de représenter correctement un Execution Plan.

Ne créer :

```go
ExecutionPlan
ExecutionStep
```

que si l'architecture actuelle rend réellement nécessaire cette distinction.

### INTERDICTION

Ne pas finir avec simultanément :

```text
Plan
ExecutionPlan
Task
ExecutionStep
```

qui représentent tous plus ou moins la même chose.

Il doit exister **un modèle canonique de plan d'exécution**.

Si `Plan` est conservé, documenter clairement :

```text
Plan = Execution Plan
Task = Execution Step
```

ou adapter les noms si nécessaire.

---

# 3. MODÈLE CIBLE

Le modèle doit pouvoir représenter au minimum :

```go
type Plan struct {
    ID      string
    Site    string
    Status  string
    Tasks   []Task
}

type Task struct {
    ID           string
    Site         string
    Target       string
    Action       string
    Method       string
    Dependencies []string
    Artifacts    []string
}
```

Adapter les champs aux conventions JSON existantes.

Les champs existants utiles comme :

```text
Reason
Version
```

peuvent être conservés s'ils ont une vraie utilité.

Ne pas supprimer une donnée existante utilisée ailleurs sans vérifier ses usages.

---

# 4. STATUTS

Le modèle de plan doit avoir des statuts explicites.

Minimum :

```text
PLANNED
EXECUTING
EXECUTED
FAILED
VERIFIED
```

Mais **ce Step ne doit produire qu'un plan `PLANNED`**.

Un planner ne doit jamais produire :

```text
EXECUTING
EXECUTED
FAILED
VERIFIED
```

simplement parce qu'il a construit le plan.

Le statut :

```text
FAILED
```

appartient à une future exécution réelle.

---

# 5. IMPORTANT — PLANIFICATION ≠ EXÉCUTION

La construction d'un plan ne signifie absolument pas :

```text
device configured
```

Elle signifie uniquement :

```text
InfraFlow sait ce qu'il faudrait faire.
```

Donc :

```text
plan built
    !=
command executed
```

et :

```text
generated artifact
    !=
device configured
```

---

# 6. ACTIONS DU PLANNER

Le planner doit utiliser un vocabulaire d'actions explicite.

Pour ce Step, le minimum demandé est :

```text
configure_interfaces
```

Exemple :

```yaml
devices:
  - name: R1
    vendor: cisco
    family: iosxe
    model: csr1000v
    network:
      interfaces:
        - name: GigabitEthernet1
          role: lan
          ipv4_mode: static
          ipv4_address: 10.0.0.1/24
        - name: GigabitEthernet2
          role: transit
          ipv4_mode: static
          ipv4_address: 10.0.1.1/30
```

doit pouvoir produire :

```text
Task:
    target: R1
    action: configure_interfaces
```

### IMPORTANT

Le planner ne doit PAS transformer automatiquement :

```text
role: wan
```

en :

```text
NAT
```

et :

```text
role: lan
```

en :

```text
DHCP
VLAN
NAT
routing
```

Les rôles d'interface sont descriptifs.

Les comportements réseau doivent être représentés par des intentions explicites dans de futures étapes.

---

# 7. RÈGLE POUR LES INTERFACES

Une action :

```text
configure_interfaces
```

doit être créée uniquement si le device possède réellement une configuration d'interfaces dans l'état désiré.

Donc :

```go
device.Network == nil
```

ou :

```go
len(device.Network.Interfaces) == 0
```

ne doit PAS créer :

```text
configure_interfaces
```

---

# 8. PAS D'ACTIONS ARTIFICIELLES

Le planner actuel ajoute :

```text
generate_inventory
generate_topology
```

automatiquement.

Dans ce Step, ne pas créer artificiellement des actions simplement parce qu'un site existe.

Exemple :

```yaml
sites:
  - name: lab
```

doit produire :

```text
0 configuration actions
```

si aucun device/intention réseau ne le justifie.

### RÈGLE

Chaque task doit être traçable à une intention réelle de l'état désiré.

Aucune task ne doit être inventée pour rendre la démonstration plus jolie.

---

# 9. MULTI-DEVICES

Exemple :

```text
R1
R2
R3
SW1
SW2
```

Le planner doit produire un ordre déterministe.

Exemple :

```text
R1
R2
R3
SW1
SW2
```

si cet ordre correspond au tri déterministe choisi.

Le choix exact du tri peut être :

```text
site
device name
action
```

mais il doit être documenté et stable.

### INTERDICTION

Ne jamais dépendre de :

```go
range map
```

pour déterminer l'ordre.

Utiliser explicitement :

```go
sort.Slice(...)
```

ou équivalent.

---

# 10. ID DÉTERMINISTE

Les IDs des tasks doivent être déterministes.

INTERDIT :

```text
UUID random
timestamp
random string
```

pour identifier une task de planning.

Exemple acceptable :

```text
plan/site-a/R1/configure_interfaces
```

ou une convention équivalente.

Le même état désiré doit produire le même ID.

---

# 11. ID DU PLAN

Le plan doit lui-même avoir un ID stable ou une convention claire.

Ne pas utiliser un UUID aléatoire simplement pour construire le plan.

Si un hash est utilisé :

```text
même desired state
    →
même logical plan ID
```

Le hash doit être déterministe.

Ne pas inclure :

```text
timestamp
random data
runtime address
```

dans le calcul.

---

# 12. METHOD / PROVIDER

Le plan doit conserver la méthode d'exécution future lorsqu'elle est explicitement définie.

Exemples :

```text
Cisco IOS XE → netconf
MikroTik RouterOS → api
FortiOS → https
```

Mais :

```text
Method != Capability verification
```

et :

```text
Method != Execution
```

Exemple :

```text
Method: netconf
Capability: UNVERIFIED
```

est parfaitement valide.

Cela signifie :

```text
InfraFlow sait quelle méthode est prévue,
mais n'affirme pas qu'elle a été testée.
```

---

# 13. UTILISATION DU CAPABILITY REGISTRY

Le repository possède déjà :

```text
CapabilityRegistry
VendorProfile
resolveCapability()
```

Ne créer aucun nouveau capability registry.

Le planner peut consulter les informations existantes pour déterminer :

```text
vendor
family
model
method
capability state
evidence
```

Mais attention :

### UNVERIFIED

Si la capability est :

```text
UNVERIFIED
```

le planner peut toujours représenter l'intention.

Il ne doit cependant jamais prétendre :

```text
supported
executed
verified
lab-tested
```

simplement parce que le planner connaît la méthode.

---

# 14. CAPABILITY ET PLANIFICATION

Ne pas confondre :

```text
PLANIFICATION
```

et :

```text
AUTORISATION D'EXÉCUTION
```

Un plan peut dire :

```text
R1
configure_interfaces
method=netconf
capability=UNVERIFIED
```

La future couche d'exécution pourra décider :

```text
execution allowed
```

ou :

```text
execution blocked
```

selon les règles de sécurité/capability.

Cette décision ne doit pas être simulée comme une exécution.

---

# 15. REASON

Le champ existant :

```go
Reason string
```

peut être conservé.

Mais il ne doit pas devenir un substitut à un modèle d'état.

Éviter :

```text
Status = blocked
Reason = ...
```

comme unique représentation d'une capability.

Si une information de capability est importante, utiliser les champs structurés existants lorsque possible.

Ne pas mettre tout le système dans des chaînes de texte.

---

# 16. ARTIFACTS

Le plan peut référencer les artefacts déjà générés.

Exemple :

```text
Artifacts:
    - site/ansible/vendor-playbook.yml
```

Cela signifie :

```text
cet artefact existe / est associé au plan
```

et absolument pas :

```text
le device a été configuré.
```

### IMPORTANT

Le planner ne doit pas appeler de génération réelle juste pour construire le plan si cela mélange :

```text
PLANNED
```

et :

```text
GENERATED
```

Si la génération existante fournit déjà un résultat consommable sans modifier le lifecycle, l'intégrer proprement.

Sinon, ne pas forcer l'intégration dans ce Step.

---

# 17. DÉPENDANCES

Le modèle doit conserver :

```go
Dependencies []string
```

ou un équivalent.

Exemple :

```text
Task A:
    R1/bootstrap

Task B:
    R1/configure_interfaces
    depends_on A
```

Mais cette étape ne doit PAS implémenter un moteur d'exécution de dépendances.

Pas de :

```text
DAG executor
BFS
DFS
retry
worker pool
parallel execution
scheduler
```

Uniquement la représentation.

---

# 18. NE PAS INVENTER DE BOOTSTRAP

Ne pas créer automatiquement :

```text
bootstrap
management
network
```

comme tasks si ces intentions ne sont pas réellement présentes dans le modèle désiré.

Une future étape pourra introduire un modèle explicite de bootstrap.

Pour ce Step :

```text
desired state
    →
actions réellement déductibles
```

et rien de plus.

---

# 19. LIFECYCLE

Le planner doit utiliser le mécanisme existant :

```go
state.Transition(StatePlanned)
```

lorsqu'un lifecycle state est disponible dans le flux concerné.

Après planning :

```text
Desired   = true
Planned   = true
Generated = false
Executed  = false
Verified  = false
Observed  = false
```

### IMPORTANT

Ne pas faire :

```go
state.Generated = true
```

dans le planner.

Ne pas faire :

```go
state.Executed = true
```

Ne pas faire :

```go
state.Verified = true
```

Ne pas faire :

```go
state.Observed = true
```

---

# 20. OBSERVED / INFERRED

Le planner ne doit jamais transformer une donnée :

```text
INFERRED
```

en :

```text
OBSERVED
```

Le planning travaille sur :

```text
DESIRED
```

et éventuellement des métadonnées/capabilities existantes.

Il ne produit aucune observation réelle.

Donc après planning :

```text
Observed = false
```

sauf si une observation réelle existait déjà dans un état externe et était explicitement fournie par l'architecture.

Le planner lui-même ne crée jamais cette observation.

---

# 21. EXEMPLE COMPLET

Entrée :

```yaml
sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: csr1000v
        provisioning:
          method: netconf
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.168.10.1/24

            - name: GigabitEthernet2
              role: transit
              ipv4_mode: static
              ipv4_address: 10.0.0.1/30

      - name: R2
        vendor: cisco
        family: iosxe
        model: csr1000v
        provisioning:
          method: netconf
        network:
          interfaces:
            - name: GigabitEthernet1
              role: transit
              ipv4_mode: static
              ipv4_address: 10.0.0.2/30
```

Plan logique attendu :

```text
Plan
    Site: lab
    Status: PLANNED

Tasks:

1.
    Target: R1
    Action: configure_interfaces
    Method: netconf

2.
    Target: R2
    Action: configure_interfaces
    Method: netconf
```

L'ordre doit être déterministe.

Le planner ne doit PAS :

```text
SSH R1
SSH R2
envoyer de commandes
appeler NETCONF
modifier R1
modifier R2
affirmer success
```

---

# 22. CAS SANS CONFIGURATION

Entrée :

```yaml
sites:
  - name: lab
```

Résultat :

```text
Plan
    Status: PLANNED
    Tasks: []
```

Aucune task inventée.

---

# 23. CAS AVEC DEVICE SANS INTERFACE

Entrée :

```yaml
devices:
  - name: R1
    vendor: cisco
    family: iosxe
    model: csr1000v
```

Résultat :

```text
aucune configure_interfaces
```

Le planner ne doit pas inventer une configuration.

---

# 24. CAS AVEC PLUSIEURS DEVICES

Entrée :

```text
R3
R1
R2
```

Résultat logique :

```text
R1
R2
R3
```

ou une autre convention explicitement définie.

Mais deux appels successifs avec le même input doivent produire exactement le même résultat sérialisé.

---

# 25. TEST DE DÉTERMINISME

Ajouter impérativement un test :

```go
first := Build(input)
second := Build(input)
```

Puis comparer une représentation déterministe :

```go
json.Marshal(first)
json.Marshal(second)
```

ou une comparaison structurée appropriée.

Résultat obligatoire :

```text
identique
```

---

# 26. TESTS OBLIGATOIRES

Remplacer/adapter les tests actuels afin qu'ils testent le nouveau contrat.

## TEST 1 — EMPTY PLAN

Input :

```text
site sans configuration réseau
```

Attendu :

```text
Status = PLANNED
Tasks = 0
```

---

## TEST 2 — CISCO INTERFACES

Device :

```text
R1
Cisco
IOS XE
CSR1000V
2 interfaces
```

Attendu :

```text
Target = R1
Action = configure_interfaces
```

---

## TEST 3 — METHOD

Si :

```text
Provisioning.Method = netconf
```

attendu :

```text
Method = netconf
```

Sans en déduire :

```text
Verified = true
```

---

## TEST 4 — MULTI-DEVICE ORDER

Input volontairement désordonné :

```text
R3
R1
R2
```

Attendu :

```text
ordre déterministe
```

---

## TEST 5 — SAME INPUT / SAME PLAN

Deux constructions successives :

```text
Build(input)
Build(input)
```

doivent être identiques.

---

## TEST 6 — NO FAKE ACTION

Un site sans network intent ne doit pas produire :

```text
generate_inventory
generate_topology
configure_interfaces
provision_device
```

simplement parce que le site/device existe.

---

## TEST 7 — LIFECYCLE

Après planning :

```text
Desired   = true
Planned   = true
Generated = false
Executed  = false
Verified  = false
Observed  = false
```

---

## TEST 8 — PLAN != EXECUTION

Le test doit démontrer qu'aucune exécution n'a lieu.

Pas de :

```text
exec.Command
os/exec
ssh
netconf
REST mutation
```

---

## TEST 9 — PLAN != VERIFICATION

Après planning :

```text
Verified == false
```

---

## TEST 10 — INFERRED != OBSERVED

Une information inférée ne doit jamais provoquer :

```text
Observed = true
```

---

## TEST 11 — DETERMINISTIC IDS

Même input :

```text
Task.ID
```

identique entre deux builds.

---

## TEST 12 — EXPLICIT INTERFACE ROLE

Tester par exemple :

```yaml
role: wan
```

et vérifier que le planner ne crée aucune action implicite :

```text
NAT
DHCP
VLAN
routing
```

---

# 27. SÉCURITÉ — INTERDICTION ABSOLUE

Le package planner ne doit introduire aucune dépendance d'exécution réseau.

Interdit :

```go
exec.Command(...)
```

```go
os/exec
```

```text
ssh
telnet
NETCONF connection
RESTCONF mutation
HTTP mutation
TCP connection
UDP connection
```

Aucune connexion réseau.

Le planner doit être une transformation pure :

```text
Desired Infrastructure
        ↓
      Planner
        ↓
   Execution Plan
```

---

# 28. VÉRIFICATION DES FICHIERS EXISTANTS

Avant modification, inspecter notamment :

```bash
sed -n '1,240p' internal/domain/plan.go
sed -n '1,320p' internal/application/planner/planner.go
sed -n '1,360p' internal/application/planner/planner_test.go
```

Puis rechercher les consommateurs :

```bash
grep -R "\.Tasks" -n --include='*.go' .
grep -R "domain.Plan" -n --include='*.go' .
grep -R "domain.Task" -n --include='*.go' .
grep -R "planner.Build" -n --include='*.go' .
```

**Ne pas casser les consommateurs existants.**

Si une modification de modèle est nécessaire, adapter proprement les consommateurs concernés.

---

# 29. NE PAS TOUCHER AUX DOMAINES FUTURS

Dans ce Step, ne pas implémenter :

```text
SSH
NETCONF execution
RESTCONF
REST API execution
Telnet
GNS3
EVE-NG
ZTP
Cisco autoinstall
PXE
iPXE
DHCP
TFTP
FTP
Proxmox
Terraform execution
Ansible execution
real device execution
retry
BFS
DFS
parallel deployment
worker pool
scheduler
drift
reconciliation
Web UI
TUI
agent communication
API
offline synchronization
metrics
observability
```

Ces sujets appartiennent à des étapes futures.

---

# 30. NE PAS MODIFIER INUTILEMENT

Ne pas réécrire :

```text
vendor_ansible.go
Cisco renderer
MikroTik renderer
FortiGate renderer
reconcile
capability registry
template registry
```

sauf si une adaptation minimale est strictement nécessaire pour compiler ou intégrer le nouveau modèle.

Le Step 01.5 concerne :

```text
domain plan model
planner
planner tests
```

principalement.

---

# 31. COMPATIBILITÉ AVEC LA GÉNÉRATION EXISTANTE

Le planner peut connaître l'existence des artefacts générés.

Mais conserver strictement :

```text
PLANNED
    !=
GENERATED
```

Donc si le planner référence :

```text
vendor-playbook.yml
```

cela ne doit pas modifier :

```text
Generated
```

dans le lifecycle.

La génération réelle sera une étape séparée.

---

# 32. RÈGLE ANTI-HALLUCINATION

Le code doit respecter :

```text
MOCK != REAL
TODO != DONE
GENERATED != EXECUTED
EXECUTED != VERIFIED
VERIFIED != LAB-TESTED
INFERRED != OBSERVED
```

Aucune sortie du planner ne doit donner l'impression qu'un équipement réel a été modifié.

---

# 33. VALIDATION FINALE

Après modification :

```bash
gofmt -w <fichiers-modifiés>

go test ./... -count=1

git diff --check

git status --short

git diff
```

Puis rechercher les appels d'exécution dans les fichiers modifiés :

```bash
grep -R "exec.Command\|os/exec\|ssh\|telnet\|netconf\|restconf\|http.NewRequest\|net.Dial\|net.Listen" -n <fichiers-modifiés>
```

Toute nouvelle exécution réelle introduite dans le planner = **STEP REFUSÉ**.

---

# 34. RAPPORT FINAL OBLIGATOIRE

Répondre uniquement avec :

```text
Fichiers modifiés:
Modèle canonique de plan:
Pourquoi ce modèle a été conservé/adapté:
Planner:
Actions supportées:
Actions supprimées car artificielles:
Déterminisme:
ID du plan:
ID des tasks:
Méthode/provider:
Capability non transformée en vérification:
Dependencies:
Artifacts:
Lifecycle PLANNED:
Generated après planning: oui/non
Executed après planning: oui/non
Verified après planning: oui/non
Observed après planning: oui/non
Exécution réelle introduite: oui/non
Tests ajoutés/modifiés:
Résultat de go test ./... -count=1:
Résultat de git diff --check:
Commit:
```

## CRITÈRE DE VALIDATION

Le Step 01.5 est validé uniquement si :

```text
[ ] un seul modèle canonique de plan existe
[ ] planner.Build() produit un Execution Plan
[ ] aucune task artificielle n'est créée
[ ] configure_interfaces fonctionne à partir d'une vraie intention réseau
[ ] aucun comportement WAN/LAN implicite n'est créé
[ ] provider/method reste descriptif
[ ] capability != verification
[ ] ordre déterministe
[ ] IDs déterministes
[ ] dependencies représentables
[ ] artifacts uniquement référencés
[ ] lifecycle = PLANNED uniquement
[ ] Generated = false
[ ] Executed = false
[ ] Verified = false
[ ] Observed = false
[ ] aucun fake data
[ ] aucune connexion réseau
[ ] aucune exécution réelle
[ ] tests unitaires complets
[ ] go test ./... passe
[ ] git diff --check passe
[ ] aucune modification inutile des providers
```

**Ne rien implémenter au-delà du Step 01.5.**
