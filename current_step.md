# Step 01.1 — PATCH RÉEL DU CODE — Cisco interfaces

## OBJECTIF UNIQUE

Tu dois **modifier le code Go du repository** pour terminer le Step 01.1.

⚠️ **NE MODIFIE PAS uniquement un fichier `.md`.**

⚠️ Le travail demandé est un **PATCH DE CODE + TESTS**.

Le commit précédent a seulement modifié la consigne du Step.
Cette fois, tu dois réellement modifier :

```text
internal/adapters/generation/vendor_ansible.go
internal/adapters/generation/vendor_ansible_test.go
```

---

## 1. CODE À TESTER

La fonction concernée existe déjà :

```go
func renderCiscoInterfaces(interfaces []domain.NetworkInterface) []any
```

Elle reçoit directement :

```text
[]domain.NetworkInterface
```

et doit produire la structure Cisco utilisée par :

```go
cisco.ios.ios_l3_interfaces
```

### Ne réécris pas inutilement cette fonction.

Si son comportement actuel est correct, conserve-le.

Le but principal de ce step est de **prouver son comportement avec des tests directs**.

---

## 2. TEST DIRECT OBLIGATOIRE

Dans :

```text
internal/adapters/generation/vendor_ansible_test.go
```

ajoute des tests qui appellent **directement** :

```go
renderCiscoInterfaces(...)
```

Exemple de principe :

```go
got := renderCiscoInterfaces(input)
```

Puis inspecte directement :

```go
got[0]
got[0]["name"]
got[0]["enabled"]
got[0]["ipv4"]
```

ou une conversion structurée équivalente.

### INTERDIT

Ne considère PAS ceci comme un test suffisant :

```go
playbook := buildVendorTasks(...)
data, _ := marshal(playbook)

strings.Contains(...)
```

Ce type de test peut rester pour les tests existants, mais il ne remplace PAS le test direct.

---

## 3. CAS DE TEST OBLIGATOIRES

### Test A — interface `/30`

Entrée :

```text
Name = GigabitEthernet2
Role = wan
IPv4Address = 10.0.0.1/30
```

Vérifier directement que la sortie contient exactement :

```text
name = GigabitEthernet2
enabled = true
ipv4[0].address = 10.0.0.1/30
```

---

### Test B — interface `/24`

Entrée :

```text
Name = GigabitEthernet3
Role = lan
IPv4Address = 10.10.10.1/24
```

Vérifier :

```text
name = GigabitEthernet3
enabled = true
ipv4[0].address = 10.10.10.1/24
```

---

### Test C — plusieurs rôles

Créer au minimum 4 interfaces :

```text
GigabitEthernet1 → management
GigabitEthernet2 → wan
GigabitEthernet3 → lan
GigabitEthernet4 → transit
```

avec des adresses réellement fournies dans le test.

Vérifier que chaque interface conserve :

```text
name
IPv4Address
enabled
```

exactement.

---

## 4. TEST INTERFACE SANS IPv4

Tester :

```text
Name = GigabitEthernet5
IPv4Address = ""
```

Vérifier que le renderer :

* conserve le nom ;
* conserve `enabled = true` si c'est son comportement actuel ;
* **n'invente aucune adresse IPv4** ;
* ne crée pas un champ `ipv4` contenant une fausse valeur.

---

## 5. TEST DU ROLE — TRÈS IMPORTANT

Le champ :

```go
Role
```

est une information descriptive.

Le renderer d'interface **NE DOIT PAS utiliser le rôle pour déclencher automatiquement une fonctionnalité réseau.**

Tester explicitement :

```text
role = wan
```

ne produit PAS :

```text
NAT
ip nat inside
ip nat outside
default route
routing
```

Et :

```text
role = lan
```

ne produit PAS automatiquement :

```text
VLAN
DHCP
NAT
routing
```

Le renderer doit seulement traduire les propriétés réellement présentes dans :

```go
domain.NetworkInterface
```

---

## 6. PAS DE ROUTING DANS CE STEP

Le test direct de :

```go
renderCiscoInterfaces()
```

doit confirmer qu'il ne produit aucune configuration :

```text
OSPF
BGP
static route
default route
```

⚠️ Ne modifie PAS `renderCiscoRoutes()`.

⚠️ N'ajoute PAS OSPF.

⚠️ N'ajoute PAS routing.

---

## 7. PAS DE NOUVELLE VALIDATION

Ne rajoute pas de validation CIDR/interface dans :

```go
renderCiscoInterfaces()
```

La validation appartient déjà au parser/validator.

Le test `/99` existant peut rester un test de :

```text
config.Parse()
```

mais il ne doit pas être considéré comme le test direct du renderer.

---

## 8. NE PAS MODIFIER

Pour ce step, ne touche absolument pas à :

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
PXE
iPXE
Proxmox
agent
Web
TUI
API
domain model
YAML schema
```

Ne crée PAS non plus :

```text
.cfg generator
native Cisco config generator
```

Ce sera un autre step.

---

## 9. FICHIERS AUTORISÉS

Tu dois modifier uniquement :

```text
internal/adapters/generation/vendor_ansible.go
internal/adapters/generation/vendor_ansible_test.go
```

Si aucun changement de `vendor_ansible.go` n'est réellement nécessaire, ne le modifies pas artificiellement.

Mais **`vendor_ansible_test.go` DOIT contenir les tests directs de `renderCiscoInterfaces()`**.

Ne modifie pas le cahier des charges pour simuler que le travail est terminé.

---

## 10. COMMANDES OBLIGATOIRES

Après modification :

```bash
gofmt -w internal/adapters/generation/vendor_ansible.go \
        internal/adapters/generation/vendor_ansible_test.go

go test ./internal/adapters/generation/... -count=1

git diff --check
```

Puis vérifie les fichiers réellement modifiés :

```bash
git status --short
git diff -- internal/adapters/generation/vendor_ansible.go
git diff -- internal/adapters/generation/vendor_ansible_test.go
```

---

## 11. CRITÈRE D'ACCEPTATION

Le step est accepté uniquement si le code contient réellement des tests de cette forme logique :

```text
NetworkInterface
      ↓
renderCiscoInterfaces()
      ↓
structure retournée
      ↓
assertions directes sur les champs
```

et non :

```text
NetworkInterface
      ↓
buildVendorTasks()
      ↓
YAML
      ↓
strings.Contains()
```

Les tests doivent échouer si `renderCiscoInterfaces()` produit :

* un mauvais nom ;
* une mauvaise adresse ;
* une mauvaise structure ;
* une adresse inventée ;
* un comportement WAN implicite ;
* un comportement LAN implicite.

---

## 12. RAPPORT FINAL

À la fin, réponds UNIQUEMENT :

```text
Fichiers modifiés:
Tests ajoutés/modifiés:
Tests exécutés:
Résultat:
```

Ne réponds pas que le Step est terminé si tu as seulement modifié un `.md`.

**Le résultat attendu est un véritable commit contenant le patch de code et les tests directs.**
