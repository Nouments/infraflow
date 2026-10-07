# InfraFlow — Step 01 : Cisco Interfaces + base ZTP

## Objectif

Implémenter le premier template Cisco réel.

Cette étape doit permettre de générer la configuration des interfaces Cisco à partir du YAML, indépendamment de leur rôle :

* management
* LAN
* WAN
* transit
* uplink
* autres interfaces réseau

Le management ne doit donc pas être considéré comme une interface spéciale dans le renderer.

## Exemple

```yaml
devices:
  - name: R1
    vendor: cisco
    family: iosxe
    model: csr1000v

    management:
      ipv4: 192.168.100.10

    network:
      interfaces:
        - name: GigabitEthernet1
          role: management
          ipv4_mode: static
          ipv4_address: 192.168.100.10/24

        - name: GigabitEthernet2
          role: lan
          ipv4_mode: static
          ipv4_address: 10.10.10.1/24

        - name: GigabitEthernet3
          role: wan
          ipv4_mode: static
          ipv4_address: 10.0.0.1/30
```

## Configuration attendue

```text
interface GigabitEthernet1
 ip address 192.168.100.10 255.255.255.0
 no shutdown

interface GigabitEthernet2
 ip address 10.10.10.1 255.255.255.0
 no shutdown

interface GigabitEthernet3
 ip address 10.0.0.1 255.255.255.252
 no shutdown
```

## ZTP / Autoinstall

Préparer l'architecture du template pour pouvoir intégrer ensuite :

* Cisco ZTP
* Cisco autoinstall
* DHCP
* TFTP/HTTP
* bootstrap configuration
* récupération de configuration initiale

Mais **ne pas implémenter tout le ZTP/autoinstall dans ce step**.

Le renderer doit simplement être conçu pour que la configuration initiale puisse être générée séparément des configurations réseau normales.

## Règles

* Ne jamais hardcoder une interface.
* Le rôle de l'interface ne doit pas déterminer si elle peut être configurée.
* Utiliser les données réelles de `DeviceNetwork.Interfaces`.
* Supporter plusieurs interfaces.
* Conversion correcte CIDR → masque Cisco.
* Refuser IPv4/CIDR invalides.
* Aucun secret dans les templates.
* Génération déterministe.
* Aucun placeholder simulant une configuration réelle.

## Tests

Tester :

* une interface management ;
* une interface LAN ;
* une interface WAN ;
* plusieurs interfaces ;
* `/24` ;
* `/30` ;
* IPv4 invalide ;
* CIDR invalide ;
* interface sans nom.

## Validation

```bash
gofmt -w .
go test ./... -count=1
git diff --check
```

Si Ansible est disponible :

```bash
ansible-playbook --syntax-check <generated-playbook>
```

## Statut

Ne jamais déclarer `LAB-VERIFIED` sans test réel sur Cisco.

Statuts possibles :

```text
IMPLEMENTED
UNIT_TESTED
INTEGRATED
LAB-VERIFIED
```

## Hors scope

Ne pas implémenter maintenant :

* static routes
* NAT
* OSPF
* DHCP server
* MikroTik
* FortiGate
* Proxmox
* ZTP complet
* autoinstall complet

Le prochain step sera décidé après validation de ce template.
