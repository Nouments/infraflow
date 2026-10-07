# Step 01.1 — Patch Cisco interface rendering

## Objectif

Corriger et compléter le Step 01 pour que le générateur Cisco configure des interfaces de manière **générique**.

Une interface ne doit PAS être considérée automatiquement comme WAN ou LAN.

Le `role` peut être `management`, `lan`, `wan`, `transit`, `uplink`, `ztp`, etc., mais le renderer d'interface ne doit pas déduire une configuration à partir de ce rôle.

Le renderer doit uniquement traduire les données réellement présentes dans `Device.Network.Interfaces`.

## À modifier

Limiter les changements à :

* `internal/adapters/generation/vendor_ansible.go`
* `internal/adapters/generation/vendor_ansible_test.go`

Ne pas modifier pour cette étape :

* MikroTik
* FortiGate
* NAT
* routing
* OSPF
* ZTP
* Autoinstall
* DHCP
* PXE/iPXE
* modèle YAML global
* agent
* API
* Web/TUI

## Tests obligatoires

Tester directement `renderCiscoInterfaces()` et vérifier la structure réellement générée.

Couvrir au minimum :

1. Interface avec IPv4 `/30`
2. Interface avec IPv4 `/24`
3. Plusieurs interfaces simultanément
4. Interfaces avec différents rôles :

   * `management`
   * `lan`
   * `wan`
   * `transit`
   * `uplink`
5. Interface sans adresse IP
6. Vérifier qu'aucune valeur n'est inventée
7. Vérifier qu'un rôle `wan` ne provoque PAS automatiquement de NAT
8. Vérifier qu'un rôle `lan` ne provoque PAS automatiquement de configuration particulière
9. Vérifier qu'aucune tâche OSPF/routing n'est générée par ce renderer
10. Vérifier les noms et adresses exacts fournis par `Device.Network.Interfaces`

Les tests doivent inspecter directement les données produites par `renderCiscoInterfaces()`, pas seulement rechercher des chaînes dans le YAML final.

## Principe attendu

Le flux doit rester :

Device.Network.Interfaces
↓
renderCiscoInterfaces()
↓
configuration Cisco d'interfaces

Et non :

role: wan
↓
NAT automatique

ou :

role: lan
↓
configuration LAN automatique

## Important

Le rôle d'une interface pourra être utilisé plus tard par d'autres fonctionnalités.

Exemples futurs :

* `uplink` → possibilité d'utiliser cette interface comme sortie Internet
* `wan` → possibilité de demander explicitement du NAT
* `transit` → possibilité de l'utiliser dans le routing
* `ztp` → bootstrap ZTP
* `management` → gestion
* `lan` → réseau interne

Mais ces comportements seront implémentés dans des étapes séparées.

## Génération

InfraFlow doit à terme pouvoir produire plusieurs types d'artefacts :

* playbook Ansible
* configuration Cisco native

Cette étape ne demande PAS encore l'implémentation du générateur `.cfg`.

Elle doit seulement garantir que la représentation des interfaces est suffisamment propre pour être réutilisée par ces deux modes de génération.

## Validation

Exécuter :

```bash
gofmt -w internal/adapters/generation/vendor_ansible.go \
        internal/adapters/generation/vendor_ansible_test.go

go test ./internal/adapters/generation/... -count=1

git diff --check
```

## Critère de réussite

Le test doit démontrer :

`Device.Network.Interfaces → renderCiscoInterfaces() → configuration Cisco`

sans hypothèse WAN/LAN et sans valeur inventée.

Rapporter uniquement :

```text
Fichiers modifiés:
Tests ajoutés/modifiés:
Résultat:
```

Ne pas implémenter d'autre fonctionnalité dans ce step.
