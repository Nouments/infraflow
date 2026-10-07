# Résumé de transition — InfraFlow

## Objectif

Continuer l’implémentation de la première tranche de génération des configurations réseau vendor dans InfraFlow, tout en conservant une séparation claire entre :

- le modèle de désir déclaré ;
- la validation stricte ;
- la génération d’artefacts Ansible ;
- l’exécution manuelle et opt-in ;
- la vérification réelle sur une infrastructure de test.

Le dépôt est destiné à rester propre : les dossiers de données locales, les artefacts générés et les fichiers de laboratoire doivent rester ignorés par Git.

## État actuel

### Implémenté et testé

- Schema réseau étendu avec :
  - `network.vdom` pour FortiGate ;
  - interfaces IPv4 statiques ou DHCP ;
  - routes IPv4 ;
  - SNAT ;
  - DNAT ;
  - services Fortinet ;
  - identifiants de politiques Fortinet ;
  - séquence de routes Fortinet.
- Validation stricte de :
  - les adresses IPv4 ;
  - les rôles d’interface ;
  - les modes IPv4 ;
  - les routes ;
  - les ports de DNAT ;
  - les plages de pools NAT ;
  - les doublons ;
  - les identifiants de politiques Fortinet ;
  - la presence du VDOM FortiGate ;
  - les restrictions sur l’usage de services Fortinet hors FortiGate.
- Génération Ansible existante et déterministe pour le rendu générique.
- Histories d’artefacts typées et validation de chemins.
- Support des artefacts vendor dans le pipeline agent :
  - `vendor-inventory.yml` ;
  - `vendor-playbook.yml` ;
  - `requirements.yml` ;
  - `vendor-template.json`.
- Garde de sécurité avec `INFRAFLOW_APPLY=true`.
- Gitignore pour :
  - `/bin/` ;
  - `/generated/` ;
  - `/provider-data/` ;
  - `/agent-data/` ;
  - `design/InfraFlow.html` ;
  - les fichiers SQLite et journaux SQLite.

### Génération vendor en cours

Le fichier [internal/adapters/generation/vendor_ansible.go](internal/adapters/generation/vendor_ansible.go) contient actuellement un renderer de base qui :

- détecte les devices avec `network` ;
- reconnaît Cisco IOS XE, MikroTik RouterOS et FortiGate FortiOS ;
- exige une adresse management IPv4 ;
- produit un inventaire, un playbook, des requirements et un manifeste ;
- marque les devices `UNVERIFIED` ;
- ajoute une garde `INFRAFLOW_APPLY=true` ;
- sans exécuter de périphérique.

Le fichier ne contient toutefois pas encore les configurations mutantes finales des trois fournisseurs. Les tâches vendor présentes sont des placeholders de sécurité/structure et ne doivent pas être considérées comme une implémentation fonctionnelle de NAT, routes, interfaces ou bootstrap.

## Limites importantes

1. **Cisco IOS XE**
   - Le rendu actuel ne produit pas une configuration NAT/DNAT fiable.
   - SNAT et DNAT doivent être étudiés avec les modules `cisco.ios` et les politiques ACL owns.
   - Un DNAT générique ne doit pas être créé sans un contrat explicite de source, ACL, interface et portée.

2. **MikroTik RouterOS**
   - Le rendu actuel ne produit pas encore les règles `ip firewall nat` et `ip route` finales.
   - Le DNAT est volontairement bloqué tant que l’ordre du `forward` chain et la priorité de filtration sont modélés.
   - Les données utilisées doivent être validées avec `community.routeros.api_modify` et la version de collection choisie.

3. **FortiGate / FortiOS**
   - Le VDOM est requis par la validation.
   - Les services Fortinet doivent préexister ou être créés explicitement ; ils ne sont pas actuellement créés automatiquement.
   - Les biais de policies, membership, VIP, IP pool et service doivent être validés contre la version FortiOS cible.
   - Le code actuel n’a pas de preuve de comportement réel sur un FortiGate.

4. **Sécurité**
   - Aucun MOT de passe n’est stocké dans les artefacts.
   - Les secrets viennent de variables d’environnement.
   - Les playbooks ne doivent jamais être exécutés automatiquement.
   - Toute configuration mutante reste en mode check ou avec opt-in explicite.

## Changements déjà présents

- Ajout de `VDOM` au modèle réseau.
- Ajout des champs IP/NAT et routes dans le modèle.
- Ajout de la validation réseau semantique.
- Ajout du renderer Ansible vendor de base.
- Intégration des artefacts vendor à `GenerateAnsible`.
- Ajout des types d’artefacts dans le protocole.
- Mise à jour des allowlists agent.
- Tests ciblés sur le modèle, la validation, le générateur et les artefacts.
- Correction du badge web et des tests associés.

## Validation exécutée

La commande suivante a été exécutée avec succès :

    go test ./... -count=1

Tous les paquets Go affichés comme tests ont terminé avec succès et aucune erreur n’a été signalée.

## État Git

Les changements suivants sont présents dans le working tree :

- fichiers de modèle et validation ;
- generator et renderer vendor ;
- artefact protocol et agent allowlists ;
- tests associés ;
- design research ;
- le nouveau fichier de résumé.

Les chemins suivants sont ignorés et ne doivent pas être ajoutés au commit :

- `/agent-data/`
- `/provider-data/`
- `/bin/`
- `/generated/`
- `design/InfraFlow.html`
- fichiers SQLite et journaux SQLite

Il faut vérifier le status Git avant le push, mais aucun dossier de données runtime n’est prévu d’être inclus.

## Prochaine tâche du prochain agent

1. Relire [internal/adapters/generation/vendor_ansible.go](internal/adapters/generation/vendor_ansible.go) et remplacer les tâches placeholders par des playbooks réellement structurés par fournisseur.
2. Ajouter des tests de rendu réels pour :
   - Cisco IOS XE SNAT ;
   - MikroTik RouterOS IPv4/interface/route ;
   - FortiGate VDOM/interface/route/NAT ;
   - manifest et hash ;
   - modes check / apply ;
   - absence de secrets.
3. Détacher les artefacts de configuration mutante de la tâche de fabrication, avec un rôle dédié :
   - `plan` ;
   - `render` ;
   - `validate` ;
   - `manual-apply`.
4. Valider la syntaxe Ansible si `ansible-playbook` ou `ansible-doc` est disponible.
5. Éviter toute création de configuration réelle tant que la validation cible n’est pas exécutée.
6. Exécuter :

    gofmt -w internal/adapters/generation/vendor_ansible.go internal/adapters/generation/generator.go internal/adapters/config/load.go internal/domain/model.go
    go test ./... -count=1
    git diff --check
    git status --short --ignored

7. Après validation, le prochain agent pourra mettre les changements en préparation dans une branche et effectuer le push demandé.

## Hints pour le prochain agent

- Ne pas confondre `vendor-inventory.yml` avec un playbook de mutation : il est un inventaire de groupe.
- Ne pas considérer `vendor-playbook.yml` comme vérifié : il est actuellement un scaffold de garde.
- Ne pas utiliser la collection version `11.5.1` / `3.22.0` / `2.6.0` sans vérifier la compatibilité exacte avec les modèles cibles.
- Ne pas lancer Ansible au cours du développement sans une configuration de test isolée.
- Ne pas supprimer les données locales ou les dossiers de données : ils sont volontairement ignorés.
- Ne pas faire de commit d’un fichier avec mot de passe, token, clé ou secret.

## Décision finale de transition

Le dépôt est prêt pour un prochain agent, mais la tranche doit être poursuivie avec la précision suivante : **suspended, non vérifiée, non exécutée**. Le code actuel ouvre les artefacts vendor, mais il ne fournit pas encore une configuration réseau fournisseur réellement fonctionnelle. La priorité est donc de compléter le rendering, d’ajouter les tests de comportement, puis de relancer la suite complète avant tout push.
