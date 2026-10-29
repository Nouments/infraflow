# InfraFlow — Cycle d’amélioration de génération déclarative

## État

- Date : 2026-10-29
- Branche : `develop`
- Commit de référence : `1107f4373439e0a34c3efb4cbd8e1fb9e39d2392`
- Travail local non commité; aucun push ni merge.
- Score du cycle précédent : **6,33/10**. Score après le cycle précédent : **6,40/10**; score courant recalculé : **6,48/10**. Les tests locaux sont verts, mais le seuil 10/10 n’est pas atteint.

## Objectif et changements de ce cycle

- Rejeter le mélange des clés racines `site` et `sites`, y compris quand `sites` est vide.
- Préserver `os_version` dans l’adaptation du schéma `NetworkDeployment` vers le modèle partagé.
- Valider la joignabilité directe des next-hops statiques : ils doivent être dans le préfixe IPv4 statique choisi ou dans un préfixe statique directement connecté; l’adresse locale, réseau et broadcast ne sont pas acceptés comme next-hop.
- Rejeter les préfixes statiques qui se chevauchent sur les interfaces d’un même équipement, les extrémités répétées/réutilisées entre liens, et exiger que le préfixe IPv4 de chaque extrémité corresponde au réseau déclaré du lien.
- Préserver `os_version` lors de l’adaptation `NetworkDeployment` → modèle partagé; valider strictement la forme de version RouterOS 7 acceptée par le renderer.
- Corriger le rendu de destination FortiOS en adresse et masque séparés.
- Rejeter les destinations de routes qui ne sont pas des réseaux IPv4 canoniques, pour garantir un rendu cohérent entre constructeurs.
- Générer les routes statiques Cisco via `cisco.ios.ios_static_routes`; vérifier le schéma contre la source officielle de la collection taguée 11.5.1 et tester le YAML réellement écrit, notamment destination CIDR, next-hop, interface et distance.
- Renseigner `7.x` dans le pilote MikroTik comme train de version, en précisant que le build et la compatibilité modèle/version restent inconnus; ajouter un test de génération du pilote vérifiant ses trois artefacts natifs et les hashes catalogués.
- Rejeter les IDs d’artefacts natifs en collision sans distinction de casse, afin d’éviter des écrasements sur des systèmes de fichiers insensibles à la casse.

## Matrice de couverture

| Domaine | État | Preuve exécutée | Limite principale |
|---|---|---|---|
| YAML mono-site/multisite et validation réseau | Implémenté, testé | Tests `config`; les formes `site` et `sites`, liens uniques, préfixes qui se chevauchent et next-hops sont testés | Routage récursif, VLAN, protocoles dynamiques et routage inter-sites non modélisés |
| Cisco, MikroTik, Fortinet natifs | Partiel, testé | Tests générateurs et `generate-all` du pilote produisent trois artefacts natifs hashés | Famille seulement; pas de matrice modèle/version ni de validation syntaxique constructeur |
| Ansible | Partiel, testé par structure | Cisco static routes générées et relues depuis le YAML écrit; argspec vérifié dans la source officielle `cisco.ios` 11.5.1 | `ansible-playbook` non exécuté; aucun test lab/appareil |
| ZTP / AutoInstall | Absent comme flux constructeur | Bootstrap générique existant, non présenté comme ZTP validé | Prérequis et procédures exactes par plateforme/modèle/version manquants |
| Catalogue matériel local | Partiel | Catalogue YAML embarqué et contrôles de ressources locales existants | Pas de synchroniseur; aucune ressource exacte et aucun droit de redistribution établis |
| Multisite / Agents | Implémenté sur le périmètre actuel | Tests d’intégration Provider-Agent et gardes de manifestes/site de la suite complète | Capacité maximale de génération fixe; la couverture future dépend de manifestes publiés |
| Sécurité / intégrité | Solide, testé | Scoping des artefacts, checksums, publication et chemins sûrs couverts par tests existants | Pas d’audit matériel, de validation externe ni de preuve de déploiement |
| Tests / documentation | Solide pour le périmètre testé | Suite complète, contrôles et génération du pilote réussis | Aucune validation sur équipement ou syntaxe native externe |

## Auto-évaluation pondérée

| Critère | Poids | Note /10 | Justification synthétique |
|---|---:|---:|---|
| A. Contrat YAML et validation réseau | 1,5 | 8,5 | Mono/multisite, identifiants, interfaces, liens uniques, préfixes et next-hops directement connectés testés; fonctions L2/L3 avancées absentes |
| B. Générateurs natifs et routage | 2,0 | 4,5 | Trois profils rendent hostname, IPv4 et routes statiques; modèle/version et syntaxe restent non certifiés, VLAN/OSPF refusés |
| C. Ansible, ZTP et AutoInstall | 1,5 | 4,5 | Interfaces et routes statiques Cisco rendues avec module versionné; playbook non exécuté, flux ZTP/AutoInstall constructeur absents |
| D. Mono-site, multisite et Agents | 1,5 | 8,0 | Publication indépendante, manifestes hashés, succès partiel et isolation Agent/site testés |
| E. Catalogue matériel local | 1,0 | 3,0 | Catalogue local embarqué présent, mais peu fourni; aucune synchronisation ni ressource binaire vérifiée |
| F. Sécurité et intégrité | 1,0 | 8,5 | Autorisation par site, validation des chemins, checksums et publication atomique couverts |
| G. Tests et preuves | 1,0 | 8,5 | Suite Go et pilotes reproductibles réussis; pas de preuve constructeur/matériel |
| H. Documentation et reproductibilité | 0,5 | 8,5 | Limites et commandes documentées; support reste à réduire à des matrices certifiées |

Calcul : `(8,5×1,5 + 4,5×2 + 4,5×1,5 + 8×1,5 + 3×1 + 8,5×1 + 8,5×1 + 8,5×0,5) / 10 = 6,48`.

## Fichiers modifiés ce cycle

- `README.md`
- `demo-output/pilot-input.yaml`
- `internal/adapters/config/load.go`
- `internal/adapters/config/load_test.go`
- `internal/adapters/config/deployment.go`
- `internal/adapters/config/deployment_test.go`
- `internal/adapters/generation/generator_test.go`
- `internal/adapters/generation/native.go`
- `internal/adapters/generation/native_test.go`
- `internal/adapters/generation/vendor_ansible.go`
- `internal/adapters/generation/vendor_ansible_test.go`
- `current_step.md`

## Vérifications réellement exécutées

- `go test -count=1 ./...` : réussi après l’intégration du rendu Cisco routes.
- `go test -race -count=1 ./internal/adapters/config ./internal/adapters/generation` : réussi.
- `make fmt-check` : réussi.
- `make vet` : réussi.
- `make build` : réussi, Provider et Agent compilés.
- `make test-scripts` : réussi, contrôles de préflight Cisco passés.
- CLI : validation du pilote réussie (`1 site`, `3 devices`); `generate-all` a produit les manifestes racine/site et trois configs natives dans un répertoire temporaire, puis ce répertoire a été supprimé.
- `git diff --check` : réussi.
- Source officielle taguée `ansible-collections/cisco.ios` `v11.5.1` : argspec `dest` CIDR, `address_families`, `next_hops`, `forward_router_address`, `interface` et `distance_metric` vérifié.
- Vérificateurs externes : `ansible-playbook` et `terraform` ne sont pas installés; leurs validations syntaxiques n’ont donc pas été exécutées.

## Blocages et prochaine action

La note de 10/10 n’est pas justifiable dans cet environnement : aucune cible matérielle/lab n’a été autorisée, aucun validateur syntaxique constructeur n’a été exécuté, et les ressources exactes/licences nécessaires au catalogue ne sont pas établies. Les versions et fonctionnalités non prouvées demeurent explicitement non vérifiées ou refusées; aucune configuration n’a été envoyée à un équipement.

Pour poursuivre vers 10/10 sans inventer de compatibilité, il faut obtenir des cibles modèle/version et des sources officielles/validateurs adaptés pour chaque profil, un lab autorisé, ainsi que des URL d’assets et droits de redistribution vérifiables. Puis ajouter les tests correspondants et réévaluer les huit critères.
