# Step 01.1 — PATCH STRICT — Cisco interfaces

## Objectif

Patcher réellement le Step 01.

Le problème actuel est que les tests vérifient surtout le YAML produit par `buildVendorTasks()`.

Ce n'est pas suffisant.

Nous devons tester directement :

```text
Device.Network.Interfaces
        ↓
renderCiscoInterfaces()
        ↓
structure Cisco générée
```

## Travail obligatoire

Modifier :

```text
internal/adapters/generation/vendor_ansible.go
internal/adapters/generation/vendor_ansible_test.go
```

### 1. Tester directement `renderCiscoInterfaces()`

Créer des tests qui appellent directement :

```go
renderCiscoInterfaces(...)
```

et inspectent la structure retournée.

Ne pas simplement faire :

```go
marshal(...)
strings.Contains(...)
```

pour considérer le test comme suffisant.

Le test doit vérifier les champs structurés :

```text
name
enabled
ipv4.address
```

avec les valeurs exactes fournies en entrée.

### 2. Cas obligatoires

Tester au minimum :

#### Interface simple

```text
GigabitEthernet2
10.0.0.1/30
```

#### Interface LAN

```text
GigabitEthernet3
10.10.10.1/24
```

#### Plusieurs interfaces

Au moins 4 interfaces avec des rôles différents :

```text
management
wan
lan
transit
```

Le rôle doit rester une information d'entrée et **ne doit pas provoquer de configuration implicite**.

### 3. Interface sans IPv4

Tester :

```text
GigabitEthernet4
IPv4Address = ""
```

Le renderer ne doit pas inventer d'adresse.

Il doit uniquement produire ce qui est réellement supporté par sa structure actuelle.

### 4. Aucun comportement implicite

Ajouter des assertions explicites démontrant que :

```text
role = wan
```

ne génère pas :

```text
NAT
ip nat inside
ip nat outside
route par défaut
```

et que :

```text
role = lan
```

ne génère pas automatiquement :

```text
VLAN
DHCP
NAT
routing
```

Le renderer d'interface reste générique.

### 5. Aucun routing

Le test doit également confirmer que l'appel à :

```go
renderCiscoInterfaces()
```

ne produit aucune configuration :

```text
OSPF
static route
default route
BGP
```

Ne pas implémenter ces fonctionnalités dans ce patch.

### 6. Validation des données invalides

Ne pas ajouter une deuxième validation dans `renderCiscoInterfaces()` si la validation existe déjà dans `config.Parse()`.

Conserver la responsabilité actuelle :

```text
config.Parse()
    ↓
validation
    ↓
renderCiscoInterfaces()
```

Le test `/99` peut rester comme test du parser, mais il ne doit pas être présenté comme un test direct du renderer.

## Important : ne pas tricher avec les tests

Interdit :

* tester uniquement la présence de chaînes dans le YAML ;
* tester uniquement `buildVendorTasks()` ;
* créer des valeurs fictives dans le renderer pour faire passer les tests ;
* modifier le modèle uniquement pour satisfaire les tests ;
* tester une fonctionnalité non implémentée ;
* considérer un test parser comme un test renderer.

Les tests doivent échouer si `renderCiscoInterfaces()` produit une mauvaise structure.

## Ne PAS toucher

Cette étape ne doit PAS modifier :

```text
MikroTik
FortiGate
NAT
routing
OSPF
BGP
ZTP
Autoinstall
DHCP
PXE/iPXE
Proxmox
agent
Web
TUI
API
```

Ne pas créer non plus le générateur `.cfg` dans cette étape.

## Commandes obligatoires

```bash
gofmt -w internal/adapters/generation/vendor_ansible.go \
        internal/adapters/generation/vendor_ansible_test.go

go test ./internal/adapters/generation/... -count=1

git diff --check
```

## Critère de validation

Le patch est accepté uniquement si les tests prouvent réellement :

```text
Device.Network.Interfaces
        ↓
renderCiscoInterfaces()
        ↓
structure Cisco exacte
```

et démontrent explicitement :

```text
WAN ≠ NAT automatique
LAN ≠ configuration automatique
role ≠ comportement implicite
```

## Rapport final obligatoire

Répondre uniquement avec :

```text
Fichiers modifiés:
Tests ajoutés/modifiés:
Tests exécutés:
Résultat:
```

Ne pas déclarer le Step terminé si le renderer lui-même n'est pas testé directement.

Ne faire aucune autre fonctionnalité.
