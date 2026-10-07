# Step 01.3 — Vérification de l'artefact Cisco généré

## Objectif

Tester le chemin réel de génération jusqu'au fichier final :

```text
DeviceNetwork.Interfaces
→ buildVendorTasks()
→ playbook Cisco
→ génération vendor-playbook.yml
→ contenu final
```

Le renderer Cisco et son intégration dans `buildVendorTasks()` sont déjà testés.

Ce step doit maintenant vérifier que les interfaces sont réellement présentes dans **l'artefact Ansible final généré par InfraFlow**.

## Règle principale

Ne pas modifier le renderer Cisco si son comportement actuel est correct.

Ne pas ajouter de nouvelle fonctionnalité réseau.

## Travail demandé

Identifier la fonction existante qui génère les fichiers :

```text
vendor-inventory.yml
vendor-playbook.yml
requirements.yml
vendor-template.json
```

et utiliser cette génération réelle dans un test.

Construire un site contenant au minimum un device Cisco IOS-XE avec :

```text
R1
vendor: cisco
family: iosxe
```

et les interfaces :

```text
GigabitEthernet1 → management → 192.168.100.10/24
GigabitEthernet2 → wan        → 10.0.0.1/30
GigabitEthernet3 → lan        → 10.10.10.1/24
GigabitEthernet4 → transit    → 10.0.0.5/30
```

## Vérifications obligatoires

Récupérer le contenu de l'artefact :

```text
vendor-playbook.yml
```

puis vérifier structurellement que le playbook contient :

```text
Render Cisco interface configuration
```

puis :

```text
cisco.ios.ios_l3_interfaces
```

puis :

```text
state: merged
```

et les quatre interfaces avec leurs adresses exactes.

Vérifier notamment :

```text
GigabitEthernet1 → 192.168.100.10/24
GigabitEthernet2 → 10.0.0.1/30
GigabitEthernet3 → 10.10.10.1/24
GigabitEthernet4 → 10.0.0.5/30
```

## Anti-hallucination

Le test doit vérifier que le fichier final ne contient pas de configuration automatiquement créée à partir du rôle :

```text
NAT
ip nat inside
ip nat outside
OSPF
BGP
static route
default route
DHCP
VLAN
```

Les rôles :

```text
management
wan
lan
transit
```

restent descriptifs.

## Important

Ne pas utiliser uniquement :

```go
strings.Contains(...)
```

pour considérer le test comme suffisant.

Parser/décoder le YAML généré et inspecter la structure correspondant à :

```text
plays
tasks
cisco.ios.ios_l3_interfaces
config
```

Les assertions doivent porter sur les champs structurés.

## Cas supplémentaire

Ajouter un cas où Cisco possède une interface :

```text
GigabitEthernet5
IPv4Address: ""
IPv4Mode: dhcp
```

Le fichier final doit contenir l'interface mais ne doit pas inventer d'adresse IPv4.

## Fichiers autorisés

Priorité :

```text
internal/adapters/generation/vendor_ansible.go
internal/adapters/generation/vendor_ansible_test.go
```

Modifier uniquement les autres fichiers si strictement nécessaire pour le test.

Ne toucher à aucun autre vendor.

## Interdit

Ne pas implémenter :

```text
OSPF
BGP
NAT
routing
VLAN
DHCP
ZTP
Autoinstall
PXE
iPXE
Proxmox
MikroTik
FortiGate
Web
TUI
API
agent
```

## Validation

Exécuter :

```bash
gofmt -w internal/adapters/generation/vendor_ansible.go \
        internal/adapters/generation/vendor_ansible_test.go

go test ./internal/adapters/generation/... -count=1

git diff --check

git status --short
```

Puis inspecter :

```bash
git diff -- internal/adapters/generation/vendor_ansible.go \
           internal/adapters/generation/vendor_ansible_test.go
```

## Rapport final

Répondre uniquement :

```text
Fichiers modifiés:
Tests ajoutés/modifiés:
Artefact final testé:
Chemin testé:
Résultat des tests:
Modification du renderer: oui/non
Commit:
```

Ne rien implémenter au-delà de ce step.
