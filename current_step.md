# PATCH — Step 01.4

## Objectif

Corriger uniquement le modèle d'état introduit dans le Step 01.4.

Le problème actuel est que `OBSERVED` dépend de `VERIFIED`.

Cette relation est incorrecte.

Une observation réelle doit pouvoir être collectée indépendamment d'une vérification.

---

# 1. Relation correcte

Le modèle doit permettre :

```text
DESIRED
   │
   ├──→ PLANNED
   │       ↓
   │    GENERATED
   │       ↓
   │    EXECUTED
   │       ↓
   │    VERIFIED
   │
   └──────────────→ OBSERVED
```

Donc :

```text
OBSERVED != VERIFIED
```

et :

```text
OBSERVED ne nécessite PAS VERIFIED
```

---

# 2. Corriger `RecordObservation`

Actuellement, le code interdit une observation réelle lorsque :

```go
!s.Verified
```

avec une erreur du type :

```text
real observation requires explicit verification first
```

Supprimer cette dépendance.

Une observation avec :

```text
ProvenanceObserved
```

doit pouvoir être enregistrée dès qu'une source externe fournit réellement l'information.

Exemple conceptuel :

```go
state := NewLifecycleState()

obs, err := state.RecordObservation(
    "GigabitEthernet1",
    "show ip interface",
    ProvenanceObserved,
    "cisco-r1",
)
```

Cela doit être valide même si :

```text
Planned   = false
Generated = false
Executed  = false
Verified  = false
```

L'observation devient alors :

```text
Observed = true
LastObservation.Provenance = OBSERVED
```

---

# 3. Ne pas modifier la signification de VERIFIED

`VERIFIED` doit rester le résultat d'une vérification explicite.

Ne pas faire :

```text
OBSERVED → VERIFIED
```

automatiquement.

Ne pas faire non plus :

```text
EXECUTED → VERIFIED
```

automatiquement.

Une future étape pourra utiliser :

```text
DESIRED
   +
OBSERVED
   ↓
verification / comparison
   ↓
VERIFIED ou DRIFT
```

---

# 4. Conserver INFERRED

Le comportement suivant doit rester obligatoire :

```text
INFERRED != OBSERVED
```

Une donnée inférée peut être enregistrée comme information dérivée, mais elle ne doit jamais positionner :

```go
Observed = true
```

et :

```go
HasObserved() == true
```

doit rester réservé à une observation réellement marquée :

```text
ProvenanceObserved
```

---

# 5. Corriger les tests

Modifier les tests existants pour refléter le nouveau comportement.

## Test obligatoire 1

Une observation réelle sans exécution doit être acceptée :

```text
NewLifecycleState()
       ↓
RecordObservation(ProvenanceObserved)
       ↓
success
       ↓
Observed = true
```

Vérifier que :

```text
Executed = false
Verified = false
Observed = true
```

est valide.

---

## Test obligatoire 2

Une donnée `INFERRED` ne doit toujours pas être considérée comme observée :

```text
RecordObservation(ProvenanceInferred)
```

doit laisser :

```text
Observed = false
HasObserved() = false
```

---

## Test obligatoire 3

Une observation réelle ne doit pas automatiquement créer `VERIFIED` :

Après :

```text
RecordObservation(ProvenanceObserved)
```

vérifier :

```text
Observed = true
Verified = false
```

---

## Test obligatoire 4

Conserver les protections existantes :

```text
DESIRED → VERIFIED
```

doit être refusé.

```text
GENERATED → VERIFIED
```

doit être refusé.

```text
EXECUTED → VERIFIED
```

reste la transition explicite autorisée.

---

# 6. Ne PAS modifier

Ne pas modifier inutilement :

```text
LifecycleState
StateDesired
StatePlanned
StateGenerated
StateExecuted
StateVerified
StateObserved
ProvenanceDesired
ProvenanceObserved
ProvenanceInferred
```

sauf si nécessaire pour corriger le comportement.

Ne pas modifier :

```text
generation
vendor_ansible
Cisco renderer
MikroTik
FortiGate
reconcile
```

---

# 7. Ne rien ajouter d'autre

NE PAS implémenter :

```text
drift detection
reconciliation
SSH
NETCONF
RESTCONF
GNS3
Cisco
ZTP
PXE
DHCP
TFTP
FTP
Proxmox
Web
TUI
agent
API
```

Ce patch corrige uniquement la relation :

```text
OBSERVED ↔ VERIFIED
```

---

# 8. Validation

Exécuter :

```bash
gofmt -w internal/domain/model.go internal/domain/model_test.go

go test ./... -count=1

git diff --check

git status --short
```

Puis inspecter :

```bash
git diff -- internal/domain/model.go internal/domain/model_test.go
```

Vérifier particulièrement qu'il n'existe plus de logique équivalente à :

```go
if !s.Verified {
    return ..., errors.New(...)
}
```

dans le chemin `ProvenanceObserved`.

---

# Rapport final

Répondre uniquement :

```text
Fichiers modifiés:
Correction effectuée:
OBSERVED sans VERIFIED: oui/non
INFERRED != OBSERVED: oui/non
Tests ajoutés/modifiés:
Résultat des tests:
Commit:
```

Ne rien implémenter au-delà de ce patch.
