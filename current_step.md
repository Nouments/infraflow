# Step 01.4 — Modèle d'état d'exécution et de vérification

## Objectif

Préparer InfraFlow à distinguer clairement :

```text
DESIRED
PLANNED
GENERATED
EXECUTED
VERIFIED
OBSERVED
```

Ce step ne doit PAS exécuter de configuration réseau réelle.

Il doit uniquement construire la base de données/modèle d'état nécessaire pour que les prochaines étapes puissent distinguer ce qui est demandé, généré, exécuté et réellement observé.

---

# Principe fondamental

InfraFlow ne doit jamais confondre :

```text
GENERATED != EXECUTED
EXECUTED != VERIFIED
VERIFIED != LAB_TESTED
```

Un fichier Ansible généré avec succès ne signifie pas que la configuration a été appliquée.

Une exécution réussie d'une commande ne signifie pas que l'état réel du device a été vérifié.

---

# Travail demandé

## 1. Inspecter l'architecture existante

Avant toute modification :

```bash
find internal -maxdepth 4 -type f | sort
```

Identifier les structures existantes concernant :

```text
domain
generation
plan
execution
deployment
state
status
audit
```

Réutiliser les structures existantes lorsque c'est possible.

Ne pas créer un deuxième système de status parallèle si un modèle existe déjà.

---

# 2. Définir les états

Créer ou compléter le modèle de domaine nécessaire pour représenter au minimum :

```text
DESIRED
PLANNED
GENERATED
EXECUTED
VERIFIED
OBSERVED
```

Les valeurs doivent être typées et explicites.

Éviter les chaînes dispersées dans tout le code.

Exemple conceptuel :

```text
DesiredState
PlanState
GenerationState
ExecutionState
VerificationState
ObservedState
```

Mais utiliser l'architecture réellement présente dans le repository plutôt que d'imposer exactement ces noms.

---

# 3. Séparer les responsabilités

Le modèle doit permettre de distinguer :

### Desired

Ce que l'utilisateur demande dans `infra.yaml`.

Exemple :

```text
GigabitEthernet1
192.168.100.10/24
```

### Planned

Ce qu'InfraFlow prévoit de faire.

Exemple :

```text
configure interface GigabitEthernet1
```

### Generated

L'artefact produit par InfraFlow.

Exemple :

```text
vendor-playbook.yml
```

### Executed

Ce qu'InfraFlow a réellement tenté d'exécuter.

Important :

```text
EXECUTED
```

ne doit pas automatiquement signifier :

```text
VERIFIED
```

### Verified

Résultat d'une vérification explicite.

Exemple :

```text
interface GigabitEthernet1
has IPv4 192.168.100.10/24
```

### Observed

État réellement observé depuis une source externe.

Exemple :

```text
device → show running-config
device → show ip interface
```

Pour le moment aucune connexion réelle n'est nécessaire.

---

# 4. Aucun faux résultat

Interdiction absolue de créer :

```text
fake device
fake execution
fake success
fake observed state
fake command output
fake metrics
fake verification
```

Ne pas ajouter de données comme :

```text
status: success
execution: completed
observed: true
```

si aucune opération réelle ne les justifie.

---

# 5. État initial

Lorsqu'une infrastructure est seulement chargée depuis YAML, elle doit représenter uniquement l'état désiré.

Exemple conceptuel :

```text
DESIRED = présent
PLANNED = absent
GENERATED = absent
EXECUTED = absent
VERIFIED = absent
OBSERVED = absent
```

Ne pas considérer une génération réussie comme une exécution.

---

# 6. Transitions

Définir des transitions cohérentes.

Exemple :

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
   ↓
OBSERVED
```

Mais ne pas permettre de transition automatique sans opération correspondante.

Par exemple :

```text
GENERATED → VERIFIED
```

ne doit pas être possible simplement parce que le YAML généré est valide.

De même :

```text
GENERATED → EXECUTED
```

ne doit pas être automatique.

---

# 7. Erreurs

Le modèle doit pouvoir représenter un échec sans le transformer en succès.

Exemple conceptuel :

```text
EXECUTION_FAILED
VERIFICATION_FAILED
```

Si le repository possède déjà un modèle d'erreur/status adapté, le réutiliser.

Ne pas créer inutilement une hiérarchie complexe.

---

# 8. Provenance

Chaque état qui représente une information externe doit pouvoir être associé à sa provenance.

Préparer le modèle pour distinguer :

```text
DESIRED
OBSERVED
INFERRED
```

Règle :

```text
INFERRED != OBSERVED
```

Une information déduite par InfraFlow ne doit jamais être présentée comme une information réellement observée sur un équipement.

---

# 9. Tests unitaires

Ajouter des tests unitaires ciblés pour vérifier :

### Test 1

Un état initial provenant uniquement du YAML ne contient pas :

```text
EXECUTED
VERIFIED
OBSERVED
```

### Test 2

Une génération d'artefact ne transforme pas automatiquement l'état en :

```text
EXECUTED
```

### Test 3

Une exécution ne transforme pas automatiquement l'état en :

```text
VERIFIED
```

### Test 4

Une information `INFERRED` n'est jamais considérée comme `OBSERVED`.

### Test 5

Les transitions invalides sont refusées.

Exemple :

```text
GENERATED → VERIFIED
```

sans exécution/vérification correspondante.

---

# 10. Ne pas implémenter maintenant

NE PAS implémenter :

```text
drift detection
reconciliation
real device execution
SSH
NETCONF
RESTCONF
GNS3
Cisco ZTP
PXE
iPXE
DHCP
TFTP
FTP
Proxmox
OSPF
BGP
NAT
VLAN
MikroTik
FortiGate
Web UI
TUI
```

Ces fonctionnalités viendront dans des steps séparés.

---

# 11. Ne pas modifier inutilement la génération Cisco

Le renderer Cisco déjà implémenté doit rester fonctionnel.

Ne pas modifier :

```text
renderCiscoInterfaces()
renderCiscoTasks()
```

sauf si une adaptation minimale est strictement nécessaire pour intégrer le nouveau modèle d'état.

Ne pas ajouter de configuration réseau.

---

# 12. Validation

Exécuter :

```bash
gofmt -w <fichiers Go modifiés>

go test ./... -count=1

git diff --check

git status --short
```

Puis vérifier :

```bash
git diff
```

Rechercher également les éventuelles données fictives ajoutées :

```bash
grep -RniE \
'fake|mock|dummy|sample|placeholder|simulat|synthetic' \
internal \
--exclude='*_test.go'
```

Cette commande ne signifie pas que tout résultat est interdit : analyser chaque résultat et vérifier qu'aucune donnée fictive n'a été introduite dans le comportement réel.

---

# 13. Contraintes de modification

Modifier uniquement les fichiers nécessaires.

Priorité aux packages existants :

```text
internal/domain/
internal/application/
internal/adapters/
```

Ne pas refactorer toute l'architecture.

Ne pas renommer massivement les packages ou structures existantes.

Ne pas ajouter de dépendance externe sauf nécessité absolue.

---

# 14. Critère de réussite

Le step est terminé uniquement si InfraFlow peut représenter explicitement la différence entre :

```text
ce que je veux
      ↓
ce que je prévois
      ↓
ce que j'ai généré
      ↓
ce que j'ai réellement exécuté
      ↓
ce que j'ai vérifié
      ↓
ce que j'ai réellement observé
```

et si les tests empêchent de présenter un état non exécuté ou non observé comme un succès réel.

---

# Rapport final

Répondre uniquement :

```text
Fichiers modifiés:
Modèle d'état ajouté/modifié:
Transitions ajoutées:
Tests ajoutés/modifiés:
Faux états empêchés:
Résultat des tests:
Dépendances ajoutées:
Commit:
```

Ne rien implémenter au-delà de ce step.
