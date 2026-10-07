# Step 01 — Review Cisco Interface Renderer

## Objectif

Revoir uniquement le renderer Cisco des interfaces.

Fichier principal :

`internal/adapters/generation/vendor_ansible.go`

## À corriger

Le renderer doit générer les interfaces uniquement depuis :

`DeviceNetwork.Interfaces`

Chaque interface peut être :

* management
* LAN
* WAN
* transit
* uplink
* autre

Le rôle ne doit pas limiter la génération.

## Interdictions

Ne pas :

* hardcoder une interface ;
* inventer une IP ;
* inventer un router-id ;
* exiger `management.ipv4` pour configurer une interface ;
* générer OSPF automatiquement ;
* modifier MikroTik/FortiGate ;
* implémenter ZTP/autoinstall maintenant.

## Exemple attendu

Input :

```yaml
interfaces:
  - name: GigabitEthernet2
    role: wan
    ipv4_mode: static
    ipv4_address: 10.0.0.1/30
```

Doit produire une configuration Cisco équivalente à :

```text
interface GigabitEthernet2
 ip address 10.0.0.1 255.255.255.252
 no shutdown
```

## Tests obligatoires

Ajouter/vérifier les tests pour :

* une interface ;
* plusieurs interfaces ;
* `/24` ;
* `/30` ;
* IP/CIDR invalide ;
* interface sans nom ;
* aucune génération automatique d'OSPF.

## Validation

```bash
gofmt -w .
go test ./... -count=1
git diff --check
```

## Important

Ne faire **aucune autre fonctionnalité** dans cette étape.

À la fin, retourner :

```text
Fichiers modifiés:
Tests:
Résultat:
Problèmes restants:
```
