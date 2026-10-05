# InfraFlow — Cahier des charges technique complet

> **Document de référence pour le développement avec Codex et les agents
> de développement.**
>
> Ce document décrit le produit, l’architecture, les contrats, les
> composants, les workflows, les templates, les mécanismes de
> provisioning, les tests, la sécurité, la roadmap et les critères
> d’acceptation.
>
> **Règle fondamentale : aucune capacité fournisseur ne doit être
> inventée.** Une fonctionnalité de provisioning ne peut être déclarée
> supportée que si elle possède une documentation de référence, un
> adapter isolé, des fixtures ou un environnement de laboratoire, des
> tests automatisés et un résultat observable.

------------------------------------------------------------------------

## 1. Vision du projet

InfraFlow est une plateforme légère d’automatisation et d’orchestration
d’infrastructures permettant de décrire une infrastructure de manière
déclarative, de générer les artefacts nécessaires, de provisionner les
équipements et serveurs, de suivre l’état du déploiement en temps réel
et de continuer à fonctionner localement lorsqu’un site perd sa
connexion avec le cloud.

InfraFlow doit couvrir progressivement :

- réseaux multi-constructeurs ;
- routeurs ;
- switches ;
- firewalls ;
- serveurs bare metal ;
- Proxmox ;
- machines virtuelles ;
- postes et équipements finaux ;
- provisioning PXE/iPXE ;
- DHCP ;
- DNS ;
- TFTP lorsque requis par un workflow ;
- transfert HTTP/HTTPS ;
- transfert de fichiers contrôlé ;
- Ansible ;
- Terraform ;
- VPN/Tailscale ;
- cloud providers ;
- inventaire matériel ;
- découverte par MAC ;
- topologie ;
- état temps réel ;
- reprise après coupure ;
- mode multisite ;
- mode local-only ;
- sécurité ;
- audit.

L’objectif n’est pas de créer un énorme monolithe qui implémente
directement chaque constructeur.

L’objectif est de construire un **moteur d’orchestration générique**
avec des **adapters spécialisés**, des contrats stables et des capacités
explicitement déclarées.

------------------------------------------------------------------------

# 2. Objectifs fonctionnels

## 2.1 Objectifs principaux

InfraFlow doit permettre :

- [x] créer une infrastructure à partir d’un fichier YAML déclaratif (socle de configuration) ;
- [x] valider le YAML avant toute action ;
- [x] détecter les erreurs de champs inconnus et les principales erreurs sémantiques ;
- [x] calculer un plan de déploiement générique ;
- [x] afficher ce plan avant exécution ;
- [x] générer les artefacts intermédiaires génériques (inventaire et topologie uniquement) ;
- [x] générer un socle Ansible générique (inventaire/playbook, sans exécution) ;
- [x] générer un socle de templates Terraform générique (état déclaré/topologie, sans provider ni apply) ;
- [x] générer les fichiers DHCP génériques à partir du réseau bootstrap ;
- [x] générer les fichiers DNS génériques à partir des adresses de management ;
- [x] générer les fichiers PXE/iPXE génériques ;
- [ ] générer les fichiers de provisioning constructeur ;
- [x] générer les scripts de bootstrap génériques (sans image ni exécution) ;
- [x] générer les inventaires génériques ;
- [ ] exécuter le provisioning ;
- [ ] suivre chaque étape ;
- [x] journaliser les opérations actuellement supportées (jobs, agents et rapports) ;
- [x] conserver les rapports d’état des artefacts génériques traités ;
- [ ] gérer les retries (le retry d’un job de planification est disponible ; les retries d’exécution et de provisioning ne le sont pas) ;
- [ ] gérer les dépendances ;
- [x] gérer plusieurs sites pour validation, planification et génération générique (sans provisioning) ;
- [ ] continuer localement hors connexion ;
- [ ] synchroniser l’état lorsque la connexion revient ;
- [x] produire une topologie déclarative à partir des liens fournis ;
- [x] fournir une TUI Linux minimale pour l’agent (jobs, agents, session API) ;
- [x] fournir une API/backend minimale pour le catalogue d’artefacts, les rapports d’état des agents, l’enregistrement/heartbeat des agents et les jobs de planification ;
- [ ] fournir une interface web ;
- [ ] exposer des événements temps réel ;
- [ ] intégrer des tests unitaires, intégration et end-to-end ;
- [ ] permettre à plusieurs agents de travailler parallèlement sans
  modifier anarchiquement les mêmes fichiers.

### Distinction importante pour le bootstrap

Les cases « générer les fichiers DHCP/DNS/PXE/iPXE » décrivent la génération
d’artefacts statiques. Elles ne préjugent pas du mode d’exécution du site.

Dans l’architecture cible, l’agent pourra fournir localement des services de
bootstrap isolés, notamment DHCP, DNS, TFTP, HTTP et iPXE, selon les capacités
et la configuration du site. Le provider génère désormais les contrats
statiques DHCP/DNS/TFTP/PXE/iPXE, mais ces services ne sont pas démarrés par le
provider. Ils devront être contrôlés, limités au workspace ou au répertoire
d’artefacts autorisé, journalisés et testés avant d’être déclarés supportés
côté agent.

------------------------------------------------------------------------

# 3. Principes non négociables

## 3.1 Declarative first

Le YAML décrit **l’état désiré**, pas une suite de commandes
impératives.

Exemple :

``` yaml
sites:
  - name: agence-01

    bootstrap:
      network: 192.168.100.0/24

    devices:
      - name: R1
        role: router
        vendor: cisco
        model: csr1000v

      - name: R2
        role: router
        vendor: mikrotik
        model: routeros

      - name: FW1
        role: firewall
        vendor: fortinet
        model: fortigate

      - name: SW1
        role: switch
        vendor: cisco

    links:
      - a: R1:Gi1
        b: R2:ether1
        network: 10.0.0.0/30
```

InfraFlow doit transformer ce modèle en plan d’exécution.

------------------------------------------------------------------------

## 3.2 Desired state vs observed state

Deux états doivent toujours être distingués :

### Desired state

Ce que le YAML demande.

### Observed state

Ce que l’agent a réellement observé.

Ne jamais remplacer silencieusement l’état désiré par l’état observé.

Exemple :

``` text
desired:
  hostname: R2
  management_ip: 10.10.0.2

observed:
  hostname: R2
  management_ip: 10.10.0.3

status:
  drift: true
```

------------------------------------------------------------------------

## 3.3 Aucun effet de bord pendant la validation

La commande de validation ne doit :

- ni modifier un équipement ;
- ni supprimer une ressource ;
- ni démarrer un provisioning ;
- ni exécuter un playbook ;
- ni lancer `terraform apply`.

Elle doit uniquement :

- parser ;
- valider ;
- normaliser ;
- détecter les contradictions ;
- produire des diagnostics.

------------------------------------------------------------------------

## 3.4 Plan avant Apply

InfraFlow doit séparer :

``` text
parse
  ↓
validate
  ↓
normalize
  ↓
resolve dependencies
  ↓
generate plan
  ↓
review
  ↓
apply
  ↓
observe
  ↓
reconcile
```

------------------------------------------------------------------------

## 3.5 Les adapters sont responsables des particularités fournisseurs

Le core ne doit jamais contenir :

``` go
if vendor == "cisco" {
    ...
}
```

partout dans le code.

Préférer :

``` go
type Provisioner interface {
    Capabilities(ctx context.Context) (Capabilities, error)
    Bootstrap(ctx context.Context, req BootstrapRequest) (Result, error)
    Configure(ctx context.Context, req ConfigureRequest) (Result, error)
    Verify(ctx context.Context, req VerifyRequest) (VerificationResult, error)
}
```

Puis :

``` text
core
 ├── cisco adapter
 ├── mikrotik adapter
 ├── fortinet adapter
 ├── proxmox adapter
 ├── linux adapter
 └── cloud adapters
```

------------------------------------------------------------------------

# 4. Architecture globale

## 4.1 Architecture logique

``` text
                         ┌─────────────────────────┐
                         │       Web Frontend      │
                         │ topology / jobs / state │
                         └────────────┬────────────┘
                                      │ HTTPS/WSS
                         ┌────────────▼────────────┐
                         │       Backend API       │
                         │ auth / projects / jobs  │
                         │ state / events / audit  │
                         └────────────┬────────────┘
                                      │
                         ┌────────────▼────────────┐
                         │     Orchestrator        │
                         │ planner / scheduler     │
                         │ DAG / BFS / DFS / retry │
                         └──────┬─────────┬────────┘
                                │         │
                     ┌──────────▼───┐ ┌──▼──────────┐
                     │ Generator    │ │ State Store │
                     │ templates    │ │ desired /   │
                     │ Ansible      │ │ observed    │
                     │ Terraform    │ │ audit       │
                     │ DHCP/PXE     │ └─────────────┘
                     └──────┬───────┘
                            │
                     ┌──────▼───────────────────┐
                     │ Provisioning adapters    │
                     │ Cisco / MikroTik / Forti │
                     │ Proxmox / Linux / Cloud  │
                     └───────────┬──────────────┘
                                 │
                       site connection / agent
                                 │
                  ┌──────────────▼──────────────┐
                  │          Site Agent         │
                  │ local orchestrator          │
                  │ local state                 │
                  │ local services              │
                  │ TUI                         │
                  │ DHCP/DNS/file service       │
                  │ Terraform/Ansible runner    │
                  └──────────────┬──────────────┘
                                 │
                 ┌───────────────▼────────────────┐
                 │       Site infrastructure      │
                 │ routers / switches / firewalls │
                 │ servers / Proxmox / PCs        │
                 └────────────────────────────────┘
```

------------------------------------------------------------------------

# 5. Déploiement cloud / on-premise / local

InfraFlow doit supporter trois modes.

## 5.1 Mode cloud

``` text
Cloud controller
      |
      +--- Site Agent A
      +--- Site Agent B
      +--- Site Agent C
```

Le cloud :

- reçoit les déclarations ;
- génère les plans ;
- suit les sites ;
- distribue les jobs ;
- reçoit les états ;
- affiche la topologie globale.

------------------------------------------------------------------------

## 5.2 Mode on-premise

Le provisioner peut être installé dans l’infrastructure locale.

``` text
LAN
 |
 +--- InfraFlow Controller
 |
 +--- Agent
 |
 +--- Devices
```

Ce mode doit fonctionner sans dépendance obligatoire à Internet.

------------------------------------------------------------------------

## 5.3 Mode local-only

Le site doit continuer à fonctionner même si le lien cloud tombe.

``` text
             INTERNET
                 X
                 |
        ┌────────▼────────┐
        │   Site Agent    │
        │ local state     │
        │ local queue     │
        │ local executor  │
        └───────┬────────┘
                |
        infrastructure
```

Pendant la coupure :

- les jobs déjà acceptés continuent ;
- l’agent peut orchestrer les opérations locales ;
- les états sont stockés localement ;
- les événements sont mis en queue ;
- les résultats sont signés/hashés ;
- aucune opération dépendant obligatoirement du cloud ne doit être
  exécutée comme si elle était disponible ;
- à la reconnexion, l’agent synchronise son état.

------------------------------------------------------------------------

# 6. Modèle de synchronisation offline

Chaque événement doit posséder un identifiant stable :

``` text
event_id
site_id
agent_id
job_id
sequence
timestamp
type
payload
hash
previous_hash
```

Exemple :

``` json
{
  "event_id": "evt-01",
  "site_id": "site-01",
  "agent_id": "agent-site-01",
  "sequence": 182,
  "type": "device.provisioned",
  "job_id": "job-42"
}
```

La synchronisation doit être idempotente.

Un événement reçu deux fois ne doit pas provoquer deux déploiements.

------------------------------------------------------------------------

# 7. Architecture Clean

## 7.1 Couches

``` text
internal/
├── domain/                         # objets et règles métier
├── application/                    # cas d’utilisation et planification
├── ports/                          # contrats possédés par les cas d’utilisation
├── adapters/                       # YAML, génération et planification concrète
├── infrastructure/                # détails runtime futurs
└── delivery/                       # entrées/sorties futures du core
```

### domain

Contient :

- Device ;
- Site ;
- Link ;
- Network ;
- Job ;
- Task ;
- Plan ;
- Capability ;
- DesiredState ;
- ObservedState ;
- Event ;
- Result.

Le domaine ne doit dépendre :

- ni de HTTP ;
- ni de SQL ;
- ni de Ansible ;
- ni de Terraform ;
- ni de SSH ;
- ni d’un constructeur.

------------------------------------------------------------------------

## 7.2 application

Contient les use cases :

``` text
CreateProject
ValidateInfrastructure
BuildPlan
GenerateArtifacts
StartDeployment
PauseDeployment
ResumeDeployment
RetryJob
GetStatus
ReconcileState
SyncAgent
RegisterAgent
```

------------------------------------------------------------------------

## 7.3 ports

Exemples :

``` go
type DeviceRepository interface {}

type JobRepository interface {}

type EventStore interface {}

type SecretStore interface {}

type Provisioner interface {}

type FileService interface {}

type DHCPService interface {}

type DNSService interface {}

type Executor interface {}

type ArtifactGenerator interface {}
```

------------------------------------------------------------------------

## 7.4 adapters

``` text
adapters/
├── cisco/
├── mikrotik/
├── fortinet/
├── proxmox/
├── linux/
├── terraform/
├── ansible/
├── dhcp/
├── dns/
├── pxe/
├── ipxe/
├── ssh/
├── http/
└── cloud/
```

------------------------------------------------------------------------

# 8. Structure de repository proposée

``` text
infraflow/
├── provider/
│   ├── cmd/
│   ├── internal/application/
│   ├── internal/adapters/{config,filesystem,generation}/
│   └── internal/delivery/{cli,grpcapi}/
├── agent/
│   ├── cmd/
│   ├── internal/config/
│   ├── internal/application/
│   ├── internal/adapters/{filesystem,processor,providergrpc}/
│   └── internal/delivery/cli/
├── internal/
│   ├── adapters/{config,generation,planning}/
│   ├── application/{planner,reconcile,scheduler}/
│   ├── domain/
│   ├── infrastructure/{safefs,security}/
│   └── ports/
├── pkg/
│   └── protocol/infraflow/v1/
├── api/proto/infraflow/v1/
├── examples/
├── Makefile
├── go.mod
├── go.sum
├── README.md
├── ARCHITECTURE.md
└── INFRAFLOW_SPEC.md
```

Le provider et l’agent sont deux binaires indépendants dans le même monorepo. L’agent ne dépend d’aucun package Go du provider : ils communiquent via le contrat gRPC versionné de `api/proto/infraflow/v1`, avec téléchargement d’artefacts en flux et configuration YAML distincte par service.

------------------------------------------------------------------------

# 9. Modèle de données

## 9.1 Site

``` yaml
site:
  id: site-01
  name: agence-01
  mode: local-capable

  bootstrap:
    network: 192.168.100.0/24
    gateway: 192.168.100.1

  services:
    dhcp: true
    dns: true
    tftp: true
    http: true
    ipxe: true
```

------------------------------------------------------------------------

## 9.2 Device

``` yaml
device:
  id: device-r1
  name: R1
  vendor: cisco
  family: router
  model: csr1000v
  role: edge-router

  identity:
    macs:
      - "00:11:22:33:44:55"

  management:
    ipv4: 10.0.0.1

  provisioning:
    method: cisco-ztp
```

Le modèle ne doit pas supposer que tous les équipements supportent le
même mécanisme.

------------------------------------------------------------------------

# 10. Matrice de capacités

Chaque adapter doit publier ses capacités.

Exemple :

``` yaml
vendor: cisco
model_family: ios-xe

capabilities:
  bootstrap:
    dhcp: true
    tftp: true
    http: true
    ztp: true
    autoinstall: true

  configuration:
    ssh: true
    netconf: unknown
    restconf: unknown

  verification:
    interfaces: true
    routing: true
    version: true
```

Valeurs autorisées :

``` text
true
false
unknown
unsupported
```

`unknown` ne doit jamais être interprété comme `true`.

------------------------------------------------------------------------

# 11. Mécanisme anti-hallucination technique

Toute fonctionnalité fournisseur doit avoir :

``` text
Capability
  ↓
Reference
  ↓
Adapter
  ↓
Fixture
  ↓
Unit tests
  ↓
Integration test
  ↓
Lab validation
  ↓
Supported
```

Une feature reste `experimental` tant que les étapes de validation ne
sont pas terminées.

------------------------------------------------------------------------

# 12. Inventory et identification

InfraFlow doit pouvoir identifier un équipement à partir de :

1.  MAC ;
2.  serial ;
3.  hostname ;
4.  management IP ;
5.  vendor ;
6.  model ;
7.  device ID préenregistré ;
8.  fingerprint observé.

Priorité recommandée :

``` text
serial exact
↓
MAC exact
↓
device identity enregistrée
↓
management IP
↓
fingerprint
↓
unknown device
```

Un équipement inconnu ne doit pas être automatiquement déployé en
production.

------------------------------------------------------------------------

# 13. Bootstrap network

Le réseau bootstrap sert uniquement à établir une première
communication.

Exemple :

``` text
192.168.100.0/24

192.168.100.1   agent/provisioner
192.168.100.10  R1
192.168.100.11  R2
192.168.100.12  SW1
192.168.100.13  FW1
```

Le bootstrap doit être temporaire lorsque cela est possible.

------------------------------------------------------------------------

# 14. DHCP

Le service DHCP InfraFlow doit supporter :

- pools ;
- reservations ;
- MAC ;
- client identifier ;
- hostname ;
- gateway ;
- DNS ;
- boot filename ;
- next server ;
- options spécifiques ;
- vendor-class ;
- user-class ;
- règles conditionnelles ;
- expiration ;
- audit des leases.

Exemple abstrait :

``` yaml
dhcp:
  networks:
    - name: bootstrap
      subnet: 192.168.100.0/24
      range:
        start: 192.168.100.100
        end: 192.168.100.200

      reservations:
        - mac: "00:11:22:33:44:55"
          ip: 192.168.100.10
          hostname: R1

      boot:
        mode: ipxe
        server: 192.168.100.1
```

Ne jamais supposer qu’un équipement utilisera automatiquement les mêmes
options DHCP qu’un autre constructeur.

### État d’implémentation

Le provider génère actuellement un contrat JSON statique: pool calculé à
partir des hôtes utilisables non réservés, réservations par MAC, gateway et
paramètres génériques de boot. Une gateway ou une réservation correspondant à
l’adresse réseau ou broadcast est rejetée. Le contrat est maintenant
consommé par un serveur DHCP agent expérimental. Celui-ci est lancé uniquement
par commande explicite, lié à une interface/IP choisie et utilise des leases
en mémoire avec une limite. Le DHCP est désactivé par défaut et exige
`services.dhcp: true`. Il implémente les réservations MAC et les échanges
DISCOVER/OFFER, REQUEST/ACK/NAK, RELEASE et DECLINE. Les leases persistantes,
le contrôle de conflits sur le réseau, les client identifiers en configuration,
options conditionnelles, vendor/user class, durées configurables et audit
restent à implémenter. Les options 66 (TFTP server name), 67 (boot filename)
et le champ BOOTP next-server sont émis selon le contrat générique; aucune
règle Cisco AutoInstall/ZTP ni option constructeur n’est fournie.
L’absence de fixtures matérielles, de validation sur réseau isolé et
d’intégration système signifie qu’aucune compatibilité DHCP constructeur ni
aptitude production n’est déclarée.

------------------------------------------------------------------------

# 15. DNS

Le DNS local doit pouvoir fournir :

``` text
r1.site-01.infraflow.local
r2.site-01.infraflow.local
sw1.site-01.infraflow.local
agent.site-01.infraflow.local
provisioner.site-01.infraflow.local
```

Fonctions :

- A ;
- AAAA ;
- PTR ;
- éventuellement CNAME ;
- zones locales ;
- reverse zones ;
- TTL ;
- validation des collisions ;
- génération déterministe.

------------------------------------------------------------------------

# 16. TFTP

TFTP doit être considéré comme un protocole de bootstrap legacy ou
spécifique, pas comme le transport général par défaut.

Utilisation :

``` text
Cisco AutoInstall
certain workflows PXE
certain bootstraps legacy
```

Pour les fichiers volumineux ou les scripts modernes, préférer lorsque
le matériel le permet :

``` text
HTTP/HTTPS
```

Le serveur TFTP doit être :

- isolé ;
- read-only par défaut ;
- limité au répertoire d’artefacts ;
- journalisé ;
- incapable d’accéder au filesystem arbitraire.

------------------------------------------------------------------------

# 17. PXE / iPXE

Architecture :

``` text
DHCP
  |
  +-- firmware/PXE client
          |
          v
      bootstrap loader
          |
          v
         iPXE
          |
          v
      HTTP/HTTPS
          |
          v
      iPXE script
          |
          v
       installer
```

InfraFlow doit générer :

``` text
pxe/
├── boot/
├── grub/
├── ipxe/
├── scripts/
├── images/
└── metadata/
```

Exemple de script abstrait :

``` ipxe
#!ipxe

set server http://192.168.100.1
kernel ${server}/images/kernel
initrd ${server}/images/initrd
imgargs kernel initrd=initrd
boot
```

Le script réel doit être généré depuis un template et validé avant
publication.

------------------------------------------------------------------------

# 18. Génération de fichiers

Le générateur doit être déterministe.

Pour un même :

``` text
infra.yaml
template version
generator version
```

il doit produire le même résultat.

Chaque artefact doit posséder :

``` text
artifact_id
type
path
generator_version
template_version
input_hash
output_hash
created_at
```

------------------------------------------------------------------------

# 19. Moteur de templates

Le moteur doit séparer :

``` text
template
+
normalized model
+
vendor/model context
+
variables
=
artifact
```

Ne jamais injecter directement le YAML utilisateur dans un template sans
validation.

------------------------------------------------------------------------

# 20. Templates Ansible

Structure :

``` text
templates/ansible/
├── inventory/
├── group_vars/
├── host_vars/
├── playbooks/
├── roles/
│   ├── common/
│   ├── network/
│   ├── cisco/
│   ├── mikrotik/
│   ├── fortinet/
│   └── linux/
└── templates/
```

Exemple :

``` text
roles/cisco_router/
├── tasks/
│   ├── main.yml
│   ├── bootstrap.yml
│   ├── interfaces.yml
│   ├── routing.yml
│   └── verification.yml
├── templates/
│   ├── hostname.j2
│   ├── interfaces.j2
│   └── routing.j2
└── defaults/
    └── main.yml
```

------------------------------------------------------------------------

# 21. Ansible network

Pour les équipements réseau, InfraFlow doit privilégier les mécanismes
Ansible Network adaptés au constructeur plutôt que d’exécuter du Python
arbitraire sur les équipements.

Exemple de modèle conceptuel :

``` yaml
- name: Configure network device
  hosts: network
  gather_facts: false
  connection: ansible.netcommon.network_cli

  tasks:
    - name: Apply generated configuration
      ansible.netcommon.cli_config:
        config: "{{ lookup('ansible.builtin.template', 'device_config.j2') }}"
```

La configuration de connexion et le `network_os` doivent être dérivés de
la capability matrix.

------------------------------------------------------------------------

# 22. Génération Terraform

Terraform doit être utilisé principalement pour les ressources qui
bénéficient d’un modèle déclaratif d’infrastructure et d’un provider
adapté.

InfraFlow ne doit pas créer un provider custom simplement pour générer
des fichiers.

Deux usages distincts :

### Usage A — Terraform comme moteur externe

InfraFlow génère :

``` text
main.tf
variables.tf
outputs.tf
providers.tf
terraform.tfvars
```

puis exécute :

``` text
terraform init
terraform validate
terraform plan
terraform apply
```

selon les permissions du job.

### Usage B — InfraFlow Terraform Provider

Si InfraFlow doit être pilotable par Terraform :

``` text
terraform-provider-infraflow
```

doit suivre l’architecture officielle des providers Terraform :

``` text
provider
resources
data sources
provider server
schema
configure
```

Ne pas mélanger le core InfraFlow et le provider Terraform.

------------------------------------------------------------------------

# 23. Structure Terraform générée

``` text
generated/
└── terraform/
    └── site-01/
        ├── versions.tf
        ├── providers.tf
        ├── variables.tf
        ├── locals.tf
        ├── main.tf
        ├── outputs.tf
        ├── terraform.tfvars.example
        └── modules/
            ├── network/
            ├── proxmox/
            └── cloud/
```

Le state Terraform ne doit pas être copié ou exposé dans les logs.

Les secrets ne doivent pas être écrits en clair dans :

``` text
.tf
.tfvars
logs
events
git
artifacts publics
```

------------------------------------------------------------------------

# 24. Proxmox

InfraFlow doit pouvoir gérer progressivement :

- cluster ;
- nodes ;
- storage ;
- bridges ;
- VLAN ;
- VM ;
- templates ;
- cloud-init ;
- snapshots ;
- tags ;
- réseau VM ;
- lifecycle ;
- inventaire.

Architecture :

``` text
InfraFlow
    |
Proxmox adapter
    |
Proxmox API
    |
cluster
```

Ne pas automatiser Proxmox par SSH lorsque l’API officielle couvre le
besoin.

------------------------------------------------------------------------

# 25. Provisioning Proxmox

Workflow :

``` text
validate desired VM
↓
resolve Proxmox node
↓
resolve storage
↓
resolve network
↓
create/clone VM
↓
configure CPU/RAM/disk
↓
configure network
↓
cloud-init/PXE si applicable
↓
start VM
↓
wait for guest
↓
discover IP
↓
verify
↓
record observed state
```

Chaque étape est idempotente autant que possible.

------------------------------------------------------------------------

# 26. Cisco

InfraFlow doit distinguer au minimum :

``` text
Cisco IOS
Cisco IOS XE
Cisco NX-OS
```

et ne doit pas considérer les trois comme identiques.

Les mécanismes possibles selon plateforme/version doivent être
représentés comme capabilities :

``` text
AutoInstall
ZTP
DHCP bootstrap
TFTP
HTTP
SSH
NETCONF
RESTCONF
```

Aucune capacité ne doit être activée uniquement parce que
`vendor=cisco`.

Le workflow Cisco doit être :

``` text
detect
↓
identify platform/version
↓
capability lookup
↓
bootstrap method selection
↓
generate required artifacts
↓
bootstrap
↓
connect
↓
apply config
↓
verify
```

------------------------------------------------------------------------

# 27. Cisco AutoInstall

Un adapter Cisco AutoInstall doit gérer séparément :

``` text
DHCP
TFTP
DNS si utilisé
network-confg / cisconet.cfg lorsque pertinent
configuration file
hostname mapping
management reachability
```

Les tests doivent utiliser des fixtures représentant :

- DHCP discover ;
- DHCP offer ;
- client identifier ;
- réservation ;
- demande de fichier ;
- téléchargement ;
- configuration résultante.

Ne pas supposer qu’un modèle moderne et un ancien IOS se comportent de
manière identique.

------------------------------------------------------------------------

# 28. Cisco ZTP

Le support ZTP doit être un capability distinct d’AutoInstall.

Architecture :

``` text
DHCP
  |
  +-- server/file location
  +-- boot/script information
        |
        v
HTTP/TFTP
        |
        v
bootstrap script
        |
        v
device initial config
```

Le driver doit vérifier la plateforme avant d’activer ce mode.

------------------------------------------------------------------------

# 29. MikroTik

MikroTik doit être séparé en plusieurs workflows :

``` text
RouterOS configuration
Netinstall
Etherboot
API/SSH
MAC-based discovery
```

Ne pas confondre :

``` text
Netinstall
```

avec un simple :

``` text
DHCP + configuration
```

Netinstall implique un workflow de boot/provisioning spécifique.

------------------------------------------------------------------------

# 30. MikroTik Netinstall

Adapter :

``` text
MikrotikNetinstallAdapter
```

Inputs :

``` yaml
device:
  mac: ...
  architecture: arm64
  routeros_version: ...
  packages:
    - routeros
    - wireless
```

Artifacts :

``` text
netinstall/
├── packages/
├── config-script.rsc
├── mode-script.rsc
└── manifest.json
```

Le driver doit pouvoir filtrer une MAC lorsque le mécanisme le permet.

Le provisioning doit être extrêmement protégé car une réinstallation
peut effacer la configuration et les données du périphérique.

------------------------------------------------------------------------

# 31. MikroTik configuration

Après bootstrap ou Netinstall :

``` text
identity
interfaces
bridge
VLAN
IP
routes
OSPF
BGP
firewall
NAT
DHCP
DNS
SNMP
NTP
users
SSH
WireGuard
QoS
```

Chaque domaine doit être un module indépendant.

------------------------------------------------------------------------

# 32. Switching

Le modèle réseau doit supporter :

``` yaml
switching:
  vlans:
    - id: 10
      name: users
    - id: 20
      name: servers

  trunks:
    - interface: ether1
      allowed_vlans: [10, 20]

  access:
    - interface: ether2
      vlan: 10

  spanning_tree:
    mode: rstp
```

Le générateur traduit ensuite vers :

``` text
Cisco
MikroTik
autres vendors
```

Le modèle logique ne doit pas contenir de commandes Cisco.

------------------------------------------------------------------------

# 33. Routage

Le modèle logique doit pouvoir représenter :

``` text
static
OSPF
BGP
VRF
L3VPN
QoS
ECMP
```

Exemple :

``` yaml
routing:
  ospf:
    router_id: 10.255.0.2
    areas:
      - id: 0.0.0.0
```

Le driver traduit vers le vendor.

------------------------------------------------------------------------

# 34. Exemple de topologie InfraFlow

``` text
                   Provisioner
                       |
                 bootstrap LAN
                       |
                      R1
                    /    \
                 R2       R3
                /  \       \
              R5   R6      SW1
                            |
                           SW2
```

Chaque lien doit être un objet :

``` yaml
links:
  - id: link-r1-r2
    endpoints:
      - device: R1
        interface: ether2
      - device: R2
        interface: ether1

    network:
      ipv4: 10.0.0.0/30
```

------------------------------------------------------------------------

# 35. BFS / DFS pour le provisioning

Le provisioning doit fonctionner comme un graphe.

## BFS

Utilisé pour :

- propagation initiale ;
- déploiement des équipements directement accessibles ;
- réduction du nombre de sauts ;
- découverte progressive.

## DFS

Utilisé lorsque :

- un équipement devient parent d’une branche ;
- il faut explorer une branche ;
- un nouveau device est découvert ;
- une branche doit être provisionnée avant d’en ouvrir d’autres.

Architecture :

``` text
bootstrap root
      |
      +-- BFS reachable devices
              |
              +-- provision R1
                    |
                    +-- discover R2
                    +-- discover R3
                           |
                           +-- DFS branch
```

------------------------------------------------------------------------

# 36. Retry

Un retry ne doit pas simplement refaire la commande.

Chaque tâche doit définir :

``` yaml
retry:
  max_attempts: 3
  backoff: exponential
  initial_delay: 2s
  max_delay: 30s
```

Erreurs :

``` text
retryable
non-retryable
unknown
```

Exemple :

``` text
connection timeout -> retryable
invalid configuration -> non-retryable
authentication failure -> généralement non-retryable
device rebooting -> retryable
unsupported capability -> non-retryable
```

------------------------------------------------------------------------

# 37. Scheduler

Le scheduler doit gérer :

``` text
dependencies
concurrency
locks
priority
retry
timeout
cancellation
resume
```

Exemple :

``` text
Task A
  ↓
Task B ──┐
         ├──> Task D
Task C ──┘
```

B et C peuvent être exécutées en parallèle si aucune contrainte ne
l’interdit.

------------------------------------------------------------------------

# 38. Locking

Empêcher deux jobs de modifier simultanément le même équipement.

Clé :

``` text
lock:device:<device_id>
```

Un second job doit recevoir :

``` text
RESOURCE_BUSY
```

ou attendre selon sa politique.

------------------------------------------------------------------------

# 39. TUI Agent

La TUI est la première interface opérationnelle de l’agent.

Elle doit être légère.

Vue principale :

``` text
INFRAFLOW AGENT
────────────────────────────────────────
Agent: agent-site-01
Site:  agence-01
Mode:  ONLINE / OFFLINE
Queue: 3
Running: 2
Failed: 0

DEVICES
✓ R1     Cisco      configured
✓ R2     MikroTik   configured
→ SW1    Cisco      provisioning
! FW1    FortiGate  failed

JOBS
#42 network-bootstrap     RUNNING
#43 proxmox-deployment     QUEUED

EVENTS
10:21 R1 reachable
10:22 R2 configured
10:23 SW1 bootstrap started
────────────────────────────────────────
q quit | r refresh | l logs | j jobs
```

------------------------------------------------------------------------

# 40. TUI modules

``` text
Dashboard
Devices
Jobs
Tasks
Logs
Events
Network
Services
Artifacts
Security
Connectivity
Diagnostics
```

La TUI ne doit pas contenir la logique métier.

Elle appelle les use cases.

------------------------------------------------------------------------

# 41. Backend API

API REST minimale :

``` text
POST   /api/v1/projects
GET    /api/v1/projects
GET    /api/v1/projects/{id}

POST   /api/v1/validate
POST   /api/v1/plan
POST   /api/v1/apply

GET    /api/v1/jobs
GET    /api/v1/jobs/{id}
POST   /api/v1/jobs/{id}/cancel
POST   /api/v1/jobs/{id}/retry

GET    /api/v1/sites
GET    /api/v1/sites/{id}

GET    /api/v1/devices
GET    /api/v1/devices/{id}

GET    /api/v1/topology

POST   /api/v1/agents/register
GET    /api/v1/agents
GET    /api/v1/agents/{id}

GET    /api/v1/events

POST   /api/v1/auth/login
POST   /api/v1/auth/logout
GET    /api/v1/auth/me
GET    /api/v1/users
POST   /api/v1/users
PATCH  /api/v1/users/{id}
```

------------------------------------------------------------------------

# 42. Temps réel

Utiliser WebSocket ou SSE pour :

``` text
job updates
task updates
device status
agent status
logs
events
topology changes
```

Exemple :

``` json
{
  "type": "task.updated",
  "job_id": "job-42",
  "task_id": "task-7",
  "status": "running",
  "progress": 60
}
```

------------------------------------------------------------------------

# 43. Web UI

La web UI doit fournir :

- dashboard ;
- sites ;
- devices ;
- topology ;
- jobs ;
- task logs ;
- artifacts ;
- desired/observed state ;
- drift ;
- agent connectivity ;
- audit ;
- security ;
- configuration editor ;
- validation ;
- plan preview.

La web UI ne doit jamais contourner les use cases backend.

------------------------------------------------------------------------

# 44. State store

Le state doit séparer :

``` text
desired_state
observed_state
execution_state
audit_state
```

Ne pas tout mélanger dans une table unique.

------------------------------------------------------------------------

# 45. Event model

Événements types :

``` text
site.created
site.updated

agent.registered
agent.connected
agent.disconnected

device.discovered
device.identified
device.bootstrap_started
device.bootstrap_completed
device.config_started
device.config_completed
device.verification_failed

job.created
job.started
job.paused
job.completed
job.failed
job.cancelled

artifact.generated
artifact.published

sync.started
sync.completed
sync.conflict
```

------------------------------------------------------------------------

# 46. Observabilité

Logs structurés JSON :

``` json
{
  "level": "info",
  "component": "orchestrator",
  "job_id": "job-42",
  "site_id": "site-01",
  "device_id": "r1",
  "event": "configuration_completed"
}
```

Métriques :

``` text
jobs_total
jobs_success_total
jobs_failed_total
task_duration_seconds
device_provisioning_duration_seconds
agent_connected
agent_queue_size
sync_events_pending
artifact_generation_errors
```

------------------------------------------------------------------------

# 47. Sécurité

## 47.1 Principes

- least privilege ;
- deny by default ;
- secrets hors Git ;
- chiffrement transport ;
- authentification forte ;
- rotation ;
- audit ;
- isolation ;
- validation stricte ;
- timeout ;
- rate limiting ;
- idempotence ;
- signature ou hash des artefacts lorsque nécessaire.

------------------------------------------------------------------------

# 48. Authentification Agent

Un agent doit être identifié par :

``` text
agent_id
site_id
credential
certificate éventuellement
```

Le modèle recommandé doit prévoir mTLS pour la communication
agent/controller lorsqu’il sera implémenté.

Le certificat doit permettre :

``` text
identity
authentication
rotation
revocation
```

Le provider implémente également une authentification humaine locale pour son
API backend : comptes stockés dans SQLite, mots de passe hachés avec bcrypt,
sessions opaques à expiration, et rôles `admin`/`user`. Le premier mot de passe
administrateur est généré par le backend lors de l’initialisation d’une base
vide, puis rendu récupérable uniquement par un script shell local protégé.
Cette authentification ne remplace pas le token machine réservé aux agents.

------------------------------------------------------------------------

# 49. Secrets

Créer une abstraction :

``` go
type SecretStore interface {
    Get(ctx context.Context, ref SecretRef) ([]byte, error)
}
```

Les use cases ne doivent pas connaître le backend de secrets.

Possibilités futures :

``` text
local encrypted store
Vault
Kubernetes Secret
cloud secret manager
```

------------------------------------------------------------------------

# 50. Sécurité des templates

Interdire :

- exécution de shell dans les templates ;
- chemins arbitraires ;
- inclusion arbitraire ;
- accès filesystem hors workspace ;
- interpolation non contrôlée ;
- secrets dans les artefacts publics.

Chaque template doit être versionné.

------------------------------------------------------------------------

# 51. Workspace d’un job

Chaque job doit disposer d’un workspace isolé :

``` text
/workspaces/
  job-42/
    input/
    generated/
    logs/
    state/
    artifacts/
```

Après expiration :

- nettoyer les secrets ;
- conserver seulement les artefacts autorisés ;
- conserver les métadonnées d’audit.

------------------------------------------------------------------------

# 52. Génération déterministe

Chaque génération doit calculer :

``` text
input_hash
template_hash
generator_hash
output_hash
```

Exemple :

``` text
SHA256(normalized_input)
SHA256(template)
SHA256(output)
```

Cela permet de détecter une modification inattendue.

------------------------------------------------------------------------

# 53. Validation YAML

Étapes :

``` text
read
↓
parse
↓
schema validation
↓
semantic validation
↓
reference validation
↓
IP/CIDR validation
↓
duplicate validation
↓
capability validation
↓
dependency validation
```

Exemples d’erreurs :

``` text
duplicate device id
duplicate MAC
invalid CIDR
overlapping networks
unknown device reference
unknown parent
unsupported provisioning method
missing management interface
invalid VLAN
missing Terraform provider
```

------------------------------------------------------------------------

# 54. IPAM

L’IPAM doit détecter :

``` text
subnet overlap
duplicate address
address outside subnet
invalid prefix
gateway collision
reservation collision
```

Plus tard :

``` text
automatic allocation
free IP tracking
IPv6
VLAN allocation
VRF-aware IPAM
```

------------------------------------------------------------------------

# 55. Topology engine

Le moteur de topologie transforme :

``` text
devices
links
interfaces
networks
routes
```

en graphe.

Il doit fournir :

``` text
neighbors
paths
connected components
parent/child
reachability
failed branches
```

La topologie affichée doit être basée sur l’état connu, et non sur des
suppositions.

------------------------------------------------------------------------

# 56. Reconciliation

Après provisioning :

``` text
desired
   |
   v
observed
   |
   v
compare
   |
   +-- equal --> healthy
   |
   +-- different --> drift
```

Exemple :

``` text
desired VLAN 20
observed VLAN 10

=> DRIFT
```

La plateforme doit afficher :

``` text
expected
actual
source
timestamp
```

------------------------------------------------------------------------

# 57. Workflow complet d’un deployment

``` text
1. User submits infra.yaml
2. Parse
3. Validate
4. Normalize
5. Resolve references
6. Detect capabilities
7. Build graph
8. Build dependency DAG
9. Generate plan
10. User/system approves
11. Create job
12. Allocate workspace
13. Generate artifacts
14. Publish bootstrap services
15. Bootstrap root devices
16. Discover children
17. Provision children
18. Configure network
19. Provision servers
20. Provision Proxmox
21. Provision workloads
22. Verify
23. Build observed state
24. Detect drift
25. Publish topology
26. Sync state
27. Close job
```

------------------------------------------------------------------------

# 58. Pipeline interne

Chaque étape doit avoir :

``` go
type Task struct {
    ID          string
    Name        string
    Dependencies []string
    Timeout     time.Duration
    Retry       RetryPolicy
    Action      TaskAction
}
```

États :

``` text
pending
queued
running
success
failed
retrying
cancelled
skipped
blocked
```

------------------------------------------------------------------------

# 59. Artifact lifecycle

``` text
generated
↓
validated
↓
published
↓
consumed
↓
verified
↓
retained/expired
```

Un artifact invalid ne doit pas être publié.

------------------------------------------------------------------------

# 60. Agent local executor

L’agent doit être capable d’exécuter localement :

``` text
Ansible
Terraform
DHCP service
DNS service
TFTP service
HTTP service
iPXE service
Netinstall adapter
SSH
local scripts contrôlés
```

Il doit aussi pouvoir :

``` text
queue jobs
resume jobs
cache artifacts
collect state
sync events
```

------------------------------------------------------------------------

# 61. Exécution des commandes

Toutes les commandes externes doivent passer par un executor contrôlé.

``` go
type CommandExecutor interface {
    Run(ctx context.Context, cmd Command) (CommandResult, error)
}
```

Le `Command` doit définir :

``` text
binary
args
environment
cwd
timeout
allowed
redacted arguments
```

Ne jamais construire une commande shell avec une concaténation de
chaînes utilisateur.

------------------------------------------------------------------------

# 62. Ansible runner

Le runner doit :

1.  créer workspace ;
2.  écrire inventory ;
3.  écrire vars ;
4.  écrire playbooks ;
5.  valider syntaxe ;
6.  exécuter ;
7.  capturer stdout/stderr ;
8.  redacter secrets ;
9.  parser résultat ;
10. produire TaskResult.

------------------------------------------------------------------------

# 63. Terraform runner

Workflow :

``` text
generate
↓
terraform fmt -check
↓
terraform validate
↓
terraform init
↓
terraform plan
↓
approval policy
↓
terraform apply
↓
terraform output
↓
state observation
```

`apply` ne doit jamais être déclenché automatiquement dans un
environnement de développement simplement parce qu’un fichier a été
généré.

------------------------------------------------------------------------

# 64. Test strategy

## 64.1 Unit tests

Minimum :

``` text
domain
parser
schema validator
IPAM
graph
planner
scheduler
retry
state comparison
template rendering
artifact hashing
event serialization
sync logic
capability selection
```

Objectif initial :

``` text
>= 80% sur le core critique
```

Le pourcentage ne doit pas remplacer la qualité des tests.

------------------------------------------------------------------------

# 65. Tests des templates

Pour chaque template :

``` text
input fixture
↓
render
↓
syntax validation
↓
golden comparison
```

Exemple :

``` text
fixtures/cisco/router-basic.yaml
expected/cisco/router-basic.cfg
```

Le test compare le résultat.

------------------------------------------------------------------------

# 66. Golden tests

Structure :

``` text
tests/
└── golden/
    ├── cisco/
    │   ├── basic.input.yaml
    │   └── basic.expected.cfg
    ├── mikrotik/
    ├── fortinet/
    ├── dhcp/
    ├── ipxe/
    └── proxmox/
```

Toute modification volontaire du template doit mettre à jour
explicitement le golden file.

------------------------------------------------------------------------

# 67. Adapter tests

Chaque adapter doit posséder :

``` text
capability test
input validation test
render test
connection error test
authentication error test
timeout test
retry test
verification test
```

------------------------------------------------------------------------

# 68. Integration tests

Tester réellement :

``` text
API ↔ orchestrator
orchestrator ↔ state store
orchestrator ↔ agent
agent ↔ executor
generator ↔ artifact store
agent ↔ local queue
sync ↔ controller
```

------------------------------------------------------------------------

# 69. E2E

Scénario minimal :

``` text
infra.yaml
↓
validate
↓
plan
↓
apply
↓
bootstrap simulated device
↓
configure
↓
verify
↓
observed state
↓
topology
```

Un laboratoire GNS3/EVE-NG peut être utilisé comme environnement
d’intégration, sans rendre le code dépendant du lab.

------------------------------------------------------------------------

# 70. Tests offline

Test obligatoire :

``` text
1. agent connected
2. start deployment
3. cut controller connection
4. agent continues
5. local tasks finish
6. events are queued
7. reconnect controller
8. sync events
9. server reconstructs state
```

Critère :

``` text
aucun événement ne doit être perdu
aucune tâche ne doit être exécutée deux fois à cause de la reconnexion
```

------------------------------------------------------------------------

# 71. Tests de sécurité

Inclure :

``` text
secret leakage
path traversal
template injection
command injection
invalid YAML
malicious YAML size
oversized artifact
unauthorized device
invalid agent credential
expired certificate
replay event
duplicate event
invalid event hash
API authorization
rate limit
```

------------------------------------------------------------------------

# 72. Fuzz testing

Go fuzzing pour :

``` text
YAML parser
IPAM
CIDR parser
template data
event decoder
sync protocol
API request parser
```

Objectif :

``` text
aucun panic
aucune corruption de state
```

------------------------------------------------------------------------

# 73. CI/CD GitLab

Pipeline :

``` text
lint
↓
format
↓
unit tests
↓
race tests
↓
security checks
↓
template tests
↓
integration tests
↓
build
↓
artifact validation
↓
package
```

Exemples de jobs :

``` text
go-fmt
go-vet
go-test
go-test-race
go-test-cover
go-staticcheck
go-build
template-test
integration-test
security-scan
```

------------------------------------------------------------------------

# 74. Race detector

Le core d’orchestration et de synchronisation doit être testé avec :

``` bash
go test -race ./...
```

Les composants concurrents doivent être conçus pour éviter :

``` text
data races
double execution
deadlock
goroutine leak
```

------------------------------------------------------------------------

# 75. Concurrence

Chaque job doit posséder son propre contexte :

``` go
ctx, cancel := context.WithCancel(parent)
```

Toutes les opérations réseau et externes doivent accepter
`context.Context`.

------------------------------------------------------------------------

# 76. Timeouts

Aucun appel réseau ou commande externe ne doit être sans timeout.

Exemples :

``` text
DHCP wait
SSH connect
HTTP download
device reboot
Ansible
Terraform
Proxmox API
sync
```

------------------------------------------------------------------------

# 77. Cancellation

Lorsqu’un job est annulé :

``` text
stop scheduling new tasks
↓
cancel running cancellable tasks
↓
preserve state
↓
emit cancellation event
↓
release locks
```

Ne jamais supprimer l’état d’un job annulé.

------------------------------------------------------------------------

# 78. Idempotence

Exemple :

``` text
ensure VLAN 20 exists
```

plutôt que :

``` text
create VLAN 20
```

L’action doit être formulée comme une convergence lorsque le backend le
permet.

------------------------------------------------------------------------

# 79. Rollback

InfraFlow doit distinguer :

``` text
rollback automatique
rollback manuel
recovery
reinstall
```

Un rollback ne doit pas être inventé pour une technologie qui ne
garantit pas une opération inverse.

Chaque adapter doit déclarer :

``` yaml
rollback:
  supported: true
  scope: configuration
```

ou :

``` yaml
rollback:
  supported: false
```

------------------------------------------------------------------------

# 80. Backups

Avant une modification destructive ou importante :

``` text
collect current config
hash
store securely
associate with job
```

Exemple :

``` text
backup/
  job-42/
    device-r1/
      running-config
      metadata.json
```

------------------------------------------------------------------------

# 81. Policy engine

Avant `apply`, InfraFlow doit pouvoir vérifier :

``` text
device approved?
vendor supported?
capability available?
destructive action?
backup exists?
maintenance window?
environment=lab/prod?
```

Exemple :

``` yaml
policy:
  deny:
    - destructive_install_without_approval
    - unknown_device_in_production
```

------------------------------------------------------------------------

# 82. Environnements

Supporter :

``` text
dev
lab
staging
production
```

Les policies peuvent varier.

Exemple :

``` text
lab:
  auto_apply: true

production:
  auto_apply: false
  approval_required: true
```

------------------------------------------------------------------------

# 83. Versionnement

Chaque objet important possède :

``` text
id
version
created_at
updated_at
```

Les artifacts doivent référencer :

``` text
input version
template version
adapter version
```

------------------------------------------------------------------------

# 84. Compatibilité

Créer une matrice :

``` text
Vendor
Family
Model
OS
OS version
Provisioning method
Configuration method
Verification method
Lab tested
Documentation verified
Status
```

Statuts :

``` text
planned
experimental
lab-tested
supported
deprecated
unsupported
```

------------------------------------------------------------------------

# 85. Exemple de matrice

``` yaml
compatibility:
  - vendor: cisco
    family: ios-xe
    method: autoinstall
    status: lab-tested

  - vendor: mikrotik
    family: routeros
    method: netinstall
    status: lab-tested

  - vendor: fortinet
    family: fortigate
    method: fortiztp
    status: experimental
```

Le status ne doit être modifié que par un test ou une validation
documentée.

------------------------------------------------------------------------

# 86. Fortinet

Fortinet doit être traité comme un adapter indépendant.

Prévoir :

``` text
FortiGate bootstrap
FortiZTP si disponible/applicable
management
configuration API/CLI
verification
```

Le support doit être conditionné par :

``` text
model
FortiOS version
licensing/cloud dependency
available provisioning mechanism
lab validation
```

Ne pas promettre une fonctionnalité FortiZTP universelle à partir du
seul vendor name.

------------------------------------------------------------------------

# 87. Cloud

Les cloud adapters doivent implémenter :

``` text
credentials
regions
networks
subnets
instances
security rules
discovery
```

Mais chaque cloud reste indépendant.

``` text
aws adapter
azure adapter
gcp adapter
```

Le core ne doit pas contenir les appels SDK spécifiques.

------------------------------------------------------------------------

# 88. Tailscale

Tailscale peut être ajouté comme adapter réseau/VPN.

Workflow :

``` text
install/provision agent
↓
authenticate
↓
join tailnet
↓
advertise routes si autorisé
↓
verify reachability
```

L’activation doit être contrôlée par policy.

------------------------------------------------------------------------

# 89. Agent packaging

Le premier objectif est un binaire léger.

Build targets :

``` text
linux/amd64
linux/arm64
linux/arm
windows/amd64
```

selon les besoins réels.

Le runtime doit éviter les dépendances inutiles.

------------------------------------------------------------------------

# 90. Deux-binaires

Architecture recommandée :

``` text
infraflow-agent
```

pour le site.

Et :

``` text
infraflow-server
```

pour le controller.

Le client CLI :

``` text
infraflow
```

peut être un troisième binaire léger.

Un futur provider Terraform peut être séparé :

``` text
terraform-provider-infraflow
```

------------------------------------------------------------------------

# 91. Agent daemon

Fonctions :

``` text
registration
heartbeat
local scheduler
local executor
local state
local artifact cache
local services
event queue
sync
health
```

Health :

``` text
GET /health
GET /ready
```

------------------------------------------------------------------------

# 92. Agent heartbeat

Exemple :

``` json
{
  "agent_id": "agent-site-01",
  "site_id": "site-01",
  "version": "0.1.0",
  "status": "healthy",
  "queue_depth": 2,
  "capabilities": [
    "dhcp",
    "dns",
    "ansible",
    "terraform",
    "pxe"
  ]
}
```

------------------------------------------------------------------------

# 93. Offline queue

Chaque tâche doit posséder :

``` text
job_id
task_id
idempotency_key
priority
created_at
expires_at
```

La queue doit survivre au redémarrage de l’agent.

------------------------------------------------------------------------

# 94. Persistance locale

Pour la première version, utiliser un store léger.

Le choix exact du moteur doit être encapsulé derrière :

``` go
StateStore
EventStore
QueueStore
```

Ainsi, le backend de stockage peut évoluer sans réécrire le domaine.

------------------------------------------------------------------------

# 95. API gRPC interne

gRPC peut être utilisé pour :

``` text
controller ↔ agent
```

Contrats :

``` text
Register
Heartbeat
ExecuteJob
StreamEvents
GetState
Sync
CancelJob
PushArtifact
```

Les messages doivent être versionnés.

------------------------------------------------------------------------

# 96. Synchronisation

Algorithme :

``` text
agent:
  get last acknowledged sequence
  send pending events
  receive missing commands
  apply commands idempotently
  acknowledge
```

En cas de conflit :

``` text
SYNC_CONFLICT
```

Ne jamais écraser silencieusement un état plus récent.

------------------------------------------------------------------------

# 97. Version des protocoles

Exemple :

``` text
protocol v1
agent API v1
event schema v1
artifact schema v1
```

Les changements incompatibles doivent créer une nouvelle version.

------------------------------------------------------------------------

# 98. Documentation obligatoire par feature

Chaque feature doit avoir :

``` text
README
architecture note
input example
output example
capability declaration
unit tests
integration test
security notes
known limitations
```

------------------------------------------------------------------------

# 99. Definition of Done

Une tâche n’est pas terminée simplement parce que le code compile.

Elle est terminée lorsque :

- [ ] code compilé ;
- [ ] tests unitaires ;
- [ ] tests d’erreur ;
- [ ] logs ;
- [ ] timeout ;
- [ ] cancellation ;
- [ ] sécurité ;
- [ ] documentation ;
- [ ] exemple ;
- [ ] intégration ;
- [ ] README mis à jour ;
- [ ] état du support mis à jour.

------------------------------------------------------------------------

# 100. Workflow obligatoire pour les agents Codex

Avant de modifier le code :

``` text
1. Lire README.md
2. Lire ARCHITECTURE.md
3. Lire INFRAFLOW_SPEC.md
4. Inspecter l'arborescence
5. Rechercher les TODO
6. Rechercher les tests existants
7. Rechercher l'implémentation existante
8. Identifier les interfaces existantes
9. Vérifier les dépendances
10. Vérifier si la feature est déjà partiellement implémentée
```

Ne jamais réimplémenter une feature existante sans vérifier son état.

------------------------------------------------------------------------

# 101. Check de README par agent

Chaque agent doit répondre avant de travailler :

``` text
[ ] README lu
[ ] architecture lue
[ ] état actuel compris
[ ] code existant recherché
[ ] tests existants recherchés
[ ] TODO recherchés
[ ] interfaces existantes recherchées
[ ] feature non dupliquée
```

------------------------------------------------------------------------

# 102. Agent de développement — rôle général

Responsabilités :

``` text
inspect
design
implement
test
document
report
```

Chaque agent doit produire un rapport :

``` text
Implemented:
- ...

Not implemented:
- ...

Tests:
- ...

Known limitations:
- ...

Files changed:
- ...

Next recommended task:
- ...
```

------------------------------------------------------------------------

# 103. Agents spécialisés

## Agent 1 — Architecture

- [x] définir les interfaces ;
- [x] définir le domaine ;
- [x] valider Clean Architecture ;
- [x] vérifier dépendances ;
- [x] éviter couplage.

## Agent 2 — Parser/schema

- [x] YAML ;
- [ ] JSON schema ;
- [x] validation ;
- [x] normalization des liens supportés ;
- [x] diagnostics.

## Agent 3 — Planner

- [ ] graph ;
- [x] DAG ;
- [ ] BFS ;
- [ ] DFS ;
- [x] dependencies ;
- [x] retry.

## Agent 4 — Generator

- [x] templates ;
- [x] artifact metadata ;
- [x] deterministic generation ;
- [ ] golden tests.

## Agent 5 — Ansible

- [x] inventory ;
- [x] playbooks ;
- [x] runner d’inspection en check mode (playbook debug allowlisté seulement) ;
- [ ] network modules ;
- [ ] result parser.

## Agent 6 — Terraform

- [x] runner `init -backend=false`/`validate` pour la déclaration data-only ;
- [x] template de représentation Terraform générique ;
- [ ] modules Terraform fournisseur ;
- [x] validation HCL et rejet des blocs provider/resource/module/data ;
- [ ] plan/apply policy ;
- [ ] state handling.

## Agent 7 — DHCP/DNS/PXE

- [x] génération DHCP statique (pool et réservations MAC) ;
- [x] génération DNS statique (A/PTR) ;
- [x] génération des métadonnées TFTP/PXE et scripts iPXE génériques ;
- [x] serveur DHCPv4 agent expérimental, commande opt-in et leases mémoire ;
- [x] serveur TFTP lecture seule borné à l’allowlist générée ;
- [x] serveur HTTP bootstrap limité aux artefacts bootstrap publiés ;
- [ ] leases persistantes, audit et options avancées DHCP ;
- [ ] validation DHCP en laboratoire isolé ;
- [ ] serveur DNS agent ;
- [ ] firmware iPXE et validation sur clients PXE réels ;
- [x] tests unitaires et transfert TFTP/HTTP en boucle locale.

## Agent 8 — Cisco

- [ ] capability detection ;
- [ ] AutoInstall ;
- [ ] ZTP ;
- [ ] SSH ;
- [ ] config generation ;
- [ ] verification ;
- [ ] lab tests.

## Agent 9 — MikroTik

- [ ] RouterOS adapter ;
- [ ] Netinstall adapter ;
- [ ] config scripts ;
- [ ] MAC handling ;
- [ ] verification ;
- [ ] lab tests.

## Agent 10 — Fortinet

- [ ] capability matrix ;
- [ ] bootstrap ;
- [ ] management ;
- [ ] configuration ;
- [ ] verification ;
- [ ] lab tests.

## Agent 11 — Proxmox

- [ ] API adapter ;
- [ ] VM lifecycle ;
- [ ] networks ;
- [ ] storage ;
- [ ] cloud-init ;
- [ ] verification.

## Agent 12 — Agent/TUI

- [ ] daemon ;
- [ ] local queue ;
- [ ] local executor ;
- [x] TUI Linux minimale ;
- [ ] offline mode ;
- [ ] sync.

## Agent 13 — Backend

- [x] REST API ;
- [ ] WebSocket/SSE ;
- [x] auth ;
- [x] RBAC ;
- [x] audit ;
- [x] jobs de planification ;

## Agent 14 — Web UI

- [ ] dashboard ;
- [ ] topology ;
- [ ] jobs ;
- [ ] devices ;
- [ ] logs ;
- [ ] desired/observed.

## Agent 15 — Security

- [ ] secrets ;
- [ ] mTLS ;
- [x] authorization ;
- [ ] command execution ;
- [ ] template security ;
- [x] audit ;
- [ ] fuzzing.

## Agent 16 — QA

- [x] unit ;
- [ ] integration ;
- [ ] e2e ;
- [ ] offline ;
- [x] race;
- [ ] golden tests ;
- [ ] compatibility matrix.

------------------------------------------------------------------------

# 104. Ordre recommandé d’implémentation

Ne pas commencer par tous les constructeurs.

## Phase 0 — Foundation

- [ ] repository ;
- [x] Go module ;
- [ ] CI ;
- [ ] lint ;
- [x] test framework ;
- [ ] logging ;
- [x] config ;
- [x] domain model.

## Phase 1 — YAML

- [ ] schema ;
- [x] parser ;
- [x] validation ;
- [x] normalization des liens supportés ;
- [x] examples.

## Phase 2 — Planner

- [ ] graph ;
- [x] DAG validation ;
- [x] task model ;
- [x] scheduler primitives ;
- [x] retry classification ;
- [x] locking.

## Phase 3 — Generator

- [x] template engine ;
- [x] artifacts ;
- [x] deterministic rendering ;
- [ ] golden tests.

## Phase 4 — Agent

- [ ] daemon ;
- [x] local state ;
- [ ] local queue ;
- [ ] executor ;
- [x] TUI Linux minimale.

## Phase 5 — Backend

- [x] API minimale (catalogue, rapports et jobs de planification) ;
- [x] enregistrement authentifié des agents, état persistant et heartbeat ;
- [ ] jobs d’exécution et de provisioning ;
- [x] journal d’événements append-only et audit des opérations supportées ;
- [x] agent registration ;
- [ ] sync.

Les primitives génériques du scheduler (DAG, dépendances, concurrence, locks,
timeouts, cancellation et retries classifiés) sont présentes dans le core, mais
elles ne sont pas encore exposées par un job d’exécution ni utilisées pour
provisionner un équipement. Les cases d’exécution restent donc décochées.

## Phase 6 — Bootstrap services

- [x] génération d’artefacts DHCP statiques côté provider ;
- [x] génération d’artefacts DNS statiques côté provider ;
- [x] génération d’artefacts TFTP statiques côté provider ;
- [x] génération d’artefacts PXE/iPXE statiques côté provider ;
- [x] service TFTP read-only avec fichiers runtime allowlistés ;
- [x] service DHCPv4 expérimental avec interface/IP explicites ;
- [x] serveur HTTP bootstrap lié à une IP d’interface et restreint aux artefacts vérifiés ;
- [ ] service DNS agent ;
- [x] serveur HTTP read-only d’artefacts vérifiés côté agent.

## Phase 7 — Ansible

- [x] inventory generator ;
- [x] playbook generator ;
- [x] runner agent restreint au debug généré et au mode check ;
- [ ] network configuration ;
- [ ] verification.

## Phase 8 — Cisco

- [ ] capability ;
- [ ] bootstrap ;
- [ ] AutoInstall ;
- [ ] ZTP ;
- [ ] config ;
- [ ] verification ;
- [ ] lab.

## Phase 9 — MikroTik

- [ ] RouterOS ;
- [ ] Netinstall ;
- [ ] configuration ;
- [ ] verification ;
- [ ] lab.

## Phase 10 — Switching

- [ ] VLAN ;
- [ ] trunk ;
- [ ] access ;
- [ ] STP ;
- [ ] LACP ;
- [ ] routing ;
- [ ] OSPF.

## Phase 11 — Fortinet

- [ ] adapter ;
- [ ] supported bootstrap ;
- [ ] config ;
- [ ] verification ;
- [ ] lab.

## Phase 12 — Proxmox

- [ ] API ;
- [ ] node ;
- [ ] storage ;
- [ ] bridge ;
- [ ] VM ;
- [ ] cloud-init ;
- [ ] verification.

## Phase 13 — Offline

- [ ] local execution ;
- [ ] persistent queue ;
- [ ] event log ;
- [ ] reconnect ;
- [ ] reconciliation ;
- [ ] conflict handling.

## Phase 14 — Web UI

- [ ] dashboard ;
- [ ] jobs ;
- [ ] devices ;
- [ ] topology ;
- [ ] logs ;
- [ ] state.

## Phase 15 — Security hardening

- [x] authentication ;
- [ ] mTLS ;
- [x] RBAC ;
- [ ] secret store ;
- [x] audit ;
- [ ] command policy ;
- [ ] fuzzing.

## Phase 16 — Production quality

- [ ] performance ;
- [ ] memory profiling ;
- [x] race testing ;
- [ ] upgrade strategy ;
- [ ] backup ;
- [ ] disaster recovery ;
- [x] documentation de l’implémentation actuelle.

------------------------------------------------------------------------

# 105. MVP

Le MVP doit être beaucoup plus petit que la vision finale.

MVP :

``` text
YAML
 ↓
validate
 ↓
plan
 ↓
generate
 ↓
agent
 ↓
DHCP
 ↓
bootstrap
 ↓
Ansible
 ↓
device configuration
 ↓
verification
 ↓
state
 ↓
TUI
```

Avec idéalement :

``` text
1 site
1 controller
1 agent
Cisco lab
MikroTik lab
```

Puis ajouter les autres systèmes.

------------------------------------------------------------------------

# 106. MVP topology

``` text
controller
    |
  agent
    |
 bootstrap network
   /   |   \
 R1   R2   SW1
```

Le premier succès d’InfraFlow doit être :

``` text
infra.yaml
→ plan
→ configuration générée
→ bootstrap
→ device reachable
→ configuration appliquée
→ état observé
```

------------------------------------------------------------------------

# 107. Exemple de YAML complet

``` yaml
version: "1"

project:
  id: demo-network
  environment: lab

sites:
  - id: site-01
    name: agence-01

    bootstrap:
      network: 192.168.100.0/24
      gateway: 192.168.100.1

    services:
      dhcp:
        enabled: true

      dns:
        enabled: true
        domain: site-01.infraflow.local

      tftp:
        enabled: true

      http:
        enabled: true

      ipxe:
        enabled: true

    devices:
      - id: r1
        name: R1
        vendor: cisco
        family: ios-xe
        role: router

        identity:
          macs:
            - "00:11:22:33:44:55"

        management:
          ipv4: 192.168.100.10

        provisioning:
          method: auto-detect

        routing:
          ospf:
            router_id: 10.255.0.1
            area: 0.0.0.0

      - id: r2
        name: R2
        vendor: mikrotik
        family: routeros
        role: router

        management:
          ipv4: 192.168.100.11

      - id: sw1
        name: SW1
        vendor: cisco
        family: ios
        role: switch

    networks:
      - id: transit-r1-r2
        cidr: 10.0.0.0/30

      - id: users
        cidr: 10.10.10.0/24
        vlan: 10

      - id: servers
        cidr: 10.10.20.0/24
        vlan: 20

    links:
      - id: link-r1-r2
        endpoints:
          - device: r1
            interface: Gi1
          - device: r2
            interface: ether1
        network: transit-r1-r2

    policies:
      require_backup_before_change: true
      require_approval_for_destructive: true
```

------------------------------------------------------------------------

# 108. Checklist globale de réalisation

## Foundation

- [ ] Clean Architecture
- [x] Domain model
- [ ] Interfaces
- [x] Config
- [ ] Logging
- [x] Errors
- [x] Context
- [ ] CI

## Configuration

- [x] YAML parser
- [ ] Schema
- [x] Validation
- [ ] IPAM
- [ ] Capability matrix

## Orchestration

- [ ] Graph
- [x] DAG
- [ ] BFS
- [ ] DFS
- [x] Scheduler
- [x] Retry
- [x] Lock
- [x] Timeout
- [x] Cancellation

## Generation

- [ ] Jinja/Tera-style templates
- [x] socle Ansible générique
- [x] socle Terraform data-only générique
- [x] artefacts bootstrap DHCP/DNS/TFTP/PXE/iPXE statiques
- [ ] modules Terraform fournisseur
- [ ] services DHCP agent
- [ ] services DNS agent
- [ ] services TFTP agent
- [ ] services PXE/iPXE agent
- [ ] iPXE
- [ ] Vendor configs

## Agent

- [ ] daemon
- [x] TUI Linux minimale
- [ ] queue
- [x] local state
- [ ] executor
- [ ] artifact cache
- [ ] offline mode
- [ ] sync

## Backend

- [x] REST
- [x] gRPC
- [ ] WebSocket/SSE
- [x] authentication
- [ ] RBAC
- [x] audit partiel
- [x] planning jobs

## Network

- [ ] Cisco
- [ ] MikroTik
- [ ] Fortinet
- [ ] VLAN
- [ ] trunk
- [ ] access
- [ ] STP
- [ ] LACP
- [ ] OSPF
- [ ] VRF
- [ ] QoS

## Servers

- [ ] PXE
- [ ] iPXE
- [ ] Linux
- [ ] bare metal
- [ ] Proxmox
- [ ] VM
- [ ] cloud-init

## Security

- [ ] secret store
- [ ] mTLS
- [ ] least privilege
- [ ] command allowlist
- [ ] template sandboxing
- [ ] audit
- [ ] signing/hash
- [ ] security tests

## Quality

- [ ] unit tests
- [ ] golden tests
- [ ] integration tests
- [ ] e2e
- [ ] offline tests
- [ ] race tests
- [ ] fuzz tests
- [ ] compatibility matrix
- [ ] lab validation

------------------------------------------------------------------------

# 109. Règles pour Codex

Codex doit respecter les règles suivantes :

1.  Lire les fichiers existants avant de créer une nouvelle abstraction.
2.  Ne jamais supprimer une implémentation fonctionnelle sans raison
    documentée.
3.  Ne jamais inventer une API constructeur.
4.  Ne jamais déclarer un vendor supporté sans test.
5.  Ne jamais hardcoder des secrets.
6.  Ne jamais concaténer une entrée utilisateur dans une commande shell.
7.  Ne jamais exécuter automatiquement une opération destructive.
8.  Ne jamais coupler le domaine à une technologie externe.
9.  Ne jamais ajouter une dépendance lourde sans justification.
10. Préférer une interface simple et testable.
11. Ajouter un test avec chaque nouvelle fonctionnalité importante.
12. Ajouter une fixture lorsqu’une intégration fournisseur est
    introduite.
13. Mettre à jour la documentation.
14. Mettre à jour la matrice de compatibilité.
15. Signaler explicitement les hypothèses.
16. Si une information technique n’est pas vérifiée, écrire `UNKNOWN` ou
    `UNVERIFIED`, pas une commande inventée.
17. Ne pas modifier plusieurs couches sans expliquer la dépendance.
18. Conserver la possibilité de fonctionnement offline.
19. Garantir l’idempotence lorsqu’elle est techniquement possible.
20. Ne jamais masquer une erreur d’un adapter.

------------------------------------------------------------------------

# 110. Règle spéciale « pas d’hallucination »

Avant d’implémenter une fonction constructeur :

``` text
QUESTION
  |
  +-- Existe-t-il une documentation fiable ?
        |
        +-- NON → status=UNKNOWN
        |
        +-- OUI
             |
             v
          fixture
             |
             v
           test
             |
             v
        lab validation
             |
             v
          supported
```

Si le comportement varie selon :

``` text
model
OS version
bootloader
license
firmware
hardware
```

ces paramètres doivent entrer dans la matrice de compatibilité.

------------------------------------------------------------------------

# 111. Documentation de référence technique

Les mécanismes suivants doivent être vérifiés contre les documentations
officielles avant implémentation finale :

- Cisco IOS/IOS XE AutoInstall ;
- Cisco IOS XE ZTP ;
- MikroTik Netinstall/RouterBOOT ;
- Fortinet FortiZTP ;
- Ansible Network ;
- Terraform Plugin Framework ;
- Proxmox API ;
- iPXE/PXE standards et documentation ;
- DHCP/TFTP/DNS selon les implémentations utilisées.

Références de départ :

- Cisco AutoInstall :
  https://www.cisco.com/c/en/us/td/docs/routers/ios-xe/system-management/system-management/m_cf-autoinstall-0.html
- Cisco IOS XE ZTP :
  https://www.cisco.com/c/en/us/td/docs/ios-xml/ios/prog/configuration/26x/26x-programmability-cg/zero-touch-provisioning.html
- MikroTik Netinstall :
  https://help.mikrotik.com/docs/spaces/ROS/pages/24805390/Netinstall
- MikroTik RouterBOARD/Etherboot :
  https://help.mikrotik.com/docs/spaces/ROS/pages/40992878/RouterBOARD
- FortiZTP : https://docs.fortinet.com/product/fortiztp
- Ansible Network :
  https://docs.ansible.com/projects/ansible/latest/network/user_guide/network_best_practices_2.5.html
- Terraform Plugin Framework :
  https://developer.hashicorp.com/terraform/plugin/framework
- Terraform provider design :
  https://developer.hashicorp.com/terraform/plugin/best-practices/hashicorp-provider-design-principles
- Proxmox documentation : https://pve.proxmox.com/pve-docs/
- iPXE documentation : https://ipxe.org/docs

------------------------------------------------------------------------

# 112. Première milestone concrète

La première milestone réellement démontrable doit être :

``` text
                  infra.yaml
                      |
                      v
                YAML validator
                      |
                      v
                    Plan
                      |
                      v
              Artifact generator
                 /          \
                /            \
           DHCP/iPXE       Ansible
                \            /
                 \          /
                    Agent
                      |
                 bootstrap LAN
                  /        \
                 R1         R2
                  \        /
                   observed
                      |
                      v
                 TUI status
```

Critères :

- [ ] YAML accepté ;
- [ ] YAML invalide rejeté ;
- [ ] plan lisible ;
- [ ] artefacts déterministes ;
- [ ] agent démarre ;
- [x] TUI affiche l’état des jobs et agents ;
- [ ] DHCP fonctionne en laboratoire ;
- [ ] bootstrap fonctionne sur au moins un équipement ;
- [ ] configuration Ansible générée ;
- [ ] résultat enregistré ;
- [ ] état observé affiché ;
- [ ] test offline réussi.

------------------------------------------------------------------------

# 113. Deuxième milestone

``` text
Cisco
+
MikroTik
+
switching
+
topology
+
retry
+
state reconciliation
```

Critères :

- [ ] deux vendors ;
- [ ] capability matrix ;
- [ ] deux adapters indépendants ;
- [ ] provisioning par graph ;
- [ ] retries ;
- [ ] verification ;
- [ ] topologie générée ;
- [ ] drift détecté.

------------------------------------------------------------------------

# 114. Troisième milestone

``` text
Fortinet
+
Proxmox
+
PXE/iPXE
+
bare metal
```

------------------------------------------------------------------------

# 115. Quatrième milestone

``` text
offline-first
+
cloud sync
+
security hardening
+
web UI
```

------------------------------------------------------------------------

# 116. Cinquième milestone

``` text
production readiness
```

Inclut :

- [ ] upgrades ;
- [ ] rollback ;
- [ ] backup ;
- [ ] HA du controller si nécessaire ;
- [ ] monitoring ;
- [ ] alerting ;
- [ ] disaster recovery ;
- [ ] documentation utilisateur ;
- [ ] documentation développeur ;
- [ ] matrice de compatibilité publiée.

------------------------------------------------------------------------

# 117. Critères de réussite du projet

InfraFlow sera considéré comme techniquement cohérent lorsque :

``` text
Un utilisateur peut :

1. décrire une infrastructure dans infra.yaml ;
2. valider sa configuration ;
3. obtenir un plan ;
4. voir les ressources et dépendances ;
5. générer les artefacts ;
6. lancer le provisioning ;
7. observer la progression ;
8. détecter les erreurs ;
9. effectuer des retries ;
10. continuer localement en cas de coupure ;
11. reconnecter l'agent ;
12. synchroniser l'état ;
13. comparer desired/observed ;
14. visualiser la topologie ;
15. vérifier les résultats ;
16. retrouver l'audit complet.
```

------------------------------------------------------------------------

# 118. Principe final d’InfraFlow

InfraFlow ne doit pas être pensé comme :

``` text
YAML → commandes shell
```

mais comme :

``` text
                 DECLARATION
                      |
                      v
                   DOMAIN
                      |
                      v
                  VALIDATION
                      |
                      v
                 NORMALIZATION
                      |
                      v
                    GRAPH
                      |
                      v
                    PLAN
                      |
          ┌───────────┴───────────┐
          │                       │
      GENERATORS               ADAPTERS
          │                       │
    Ansible/Terraform      Cisco/MikroTik/Forti
    DHCP/DNS/PXE           Proxmox/Linux/Cloud
          │                       │
          └───────────┬───────────┘
                      |
                    AGENT
                      |
              LOCAL EXECUTION
                      |
             OBSERVED STATE
                      |
                 RECONCILE
                      |
                  EVENTS
                      |
                   SYNC
                      |
                 CONTROLLER
                      |
                 WEB / TUI
```

La valeur centrale d’InfraFlow est donc la combinaison :

``` text
Declarative Infrastructure
+
Dependency-aware Orchestration
+
Multi-vendor Adapters
+
Local-first Execution
+
Offline Continuity
+
Observed State
+
Reconciliation
+
Secure Automation
+
Testable Architecture
```

**Tout ce qui n’est pas vérifié doit rester explicitement marqué comme
non vérifié.**
