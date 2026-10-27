# INFRAFLOW — MISSION QA ET FIABILISATION POUR ATTEINDRE 80/100

## Objectif

Tu interviens sur le dépôt existant `Nouments/infraflow`, branche `develop`.

La dernière évaluation du projet est de 74/100. L'objectif de cette mission est d'améliorer le projet vers 80/100 en traitant les lacunes les plus importantes avec des modifications concrètes, testées et documentées.

Ce score est un objectif de travail, pas un résultat garanti. Tu ne dois jamais gonfler la note en ajoutant des fonctionnalités fictives ou en déclarant réussis des tests non exécutés.

## Règles non négociables

1. Inspecte l'état actuel de la branche, `CURRENT.md`, `README.md`, `INFRAFLOW_SPEC.md`, les tests et le dernier commit avant toute modification.
2. Préserve les changements préexistants de l'utilisateur.
3. Ne réécris pas l'architecture et ne supprime aucune fonctionnalité opérationnelle.
4. N'ajoute pas de données de démonstration présentées comme réelles.
5. N'invente aucune compatibilité de modèle, version logicielle, interface ou fonctionnalité réseau.
6. Ne considère pas qu'un template fonctionne sur un équipement physique parce qu'il se compile ou produit du texte.
7. Ne fais aucun commit ni push sans autorisation explicite.
8. Priorise les défauts reproductibles, puis les tests de régression et la documentation.

## Étape 1 — Établir un état initial fiable

Inspecte notamment :

- `internal/adapters/catalog/`
- `internal/adapters/generation/`
- `internal/adapters/config/`
- `provider/internal/adapters/executors/`
- `provider/internal/delivery/httpapi/`
- `provider/internal/delivery/grpcapi/`
- `agent/internal/`
- les fichiers YAML d'exemple ;
- les tests existants et le guide de lab Cisco.

Exécute les contrôles pertinents disponibles :

```bash
git status --short --branch
git log -1 --oneline
go test -count=1 ./...
make fmt-check
make vet
make build
git diff --check
```

Note les résultats exacts, les commandes non disponibles et les éventuels défauts reproductibles.

Présente ensuite un plan très court, classé par priorité. Si un défaut critique apparaît, corrige-le avant de passer aux améliorations secondaires.

## Étape 2 — Renforcer le catalogue et la compatibilité

Le catalogue déclaratif actuel est limité et le template Cisco ISR 1100 ne couvre que l'adressage IPv4 statique et l'état administratif des interfaces.

Améliore ce périmètre sans prétendre implémenter tout le réseau.

1. Vérifie que chaque fiche d'équipement possède un identifiant unique, des métadonnées valides et des références de templates cohérentes.
2. Vérifie que chaque template référencé existe, que ses métadonnées sont valides et que les capacités annoncées correspondent aux variables réellement traitées.
3. Refuse les références de templates inexistantes, les identifiants dupliqués et les combinaisons incompatibles.
4. Vérifie que la sélection ne peut pas retenir un template uniquement parce qu'il obtient un score élevé alors qu'une condition obligatoire échoue.
5. Conserve la distinction entre `verified`, `unknown`, `unverified` et `render-only` selon les conventions du projet.
6. Ajoute quelques fiches supplémentaires uniquement si le modèle exact, les informations et les sources officielles sont vérifiables.
7. Ne marque aucune fonctionnalité comme supportée simplement parce qu'elle figure dans une fiche technique.

N'ajoute pas de modèles fictifs pour augmenter artificiellement le nombre d'équipements.

## Étape 3 — Tests de régression prioritaires

Ajoute ou améliore des tests ciblés pour les cas suivants :

- YAML mal formé et champs obligatoires absents ;
- modèle absent du catalogue ;
- template manquant ou métadonnées invalides ;
- fonction réseau non prise en charge ;
- adresses IPv4 et préfixes invalides ;
- sélection déterministe d'un template ;
- rapport cohérent avec les fichiers effectivement générés ;
- intégrité SHA-256 des artefacts ;
- absence d'artefacts partiels après échec ;
- chemins de ressources locales invalides ou fichiers manquants ;
- accès API non autorisé et séparation des rôles ;
- comportement de l'Agent lors d'un artefact corrompu.

Réutilise les tests existants et ajoute uniquement les tests manquants. N'affaiblis pas les assertions existantes pour faire passer la suite.

Pour toute correction, écris d'abord un test qui reproduit le défaut lorsque cela est raisonnablement possible, puis vérifie qu'il passe après la correction.

## Étape 4 — Démonstration de bout en bout reproductible

Prépare un parcours simple à partir des exemples versionnés :

1. Valider une déclaration YAML.
2. Générer la configuration déclarative.
3. Inspecter la configuration et le rapport JSON.
4. Tester une fonction non prise en charge et vérifier le rejet.
5. Vérifier qu'aucun artefact partiel n'est publié après l'échec.
6. Montrer la différence entre génération, planification, exécution et vérification.
7. Vérifier les sommes SHA-256 des artefacts concernés.

Documente les commandes exactes et les résultats attendus dans la documentation appropriée.

Utilise des répertoires temporaires pour les tests et la démonstration. Ne supprime jamais les fichiers préexistants de l'utilisateur.

## Étape 5 — Vérifier les exécutables et les contrats

Si les outils nécessaires sont disponibles, valide aussi les sorties produites par les générateurs Ansible et Terraform :

- Ansible : contrôle syntaxique avec les collections requises.
- Terraform : `fmt -check`, `init -backend=false` et `validate`.
- Provider et Agent : compilation et tests.
- Cisco IOS : test en lab uniquement si un équipement autorisé est réellement accessible.

Si un outil est absent, signale-le explicitement. N'affirme jamais qu'un contrôle a été effectué lorsque seule une vérification textuelle a été faite.

N'exécute pas `terraform apply`, de playbook mutateur ou de configuration sur un équipement sans autorisation explicite.

## Étape 6 — Sécurité et qualité

Vérifie les points à fort impact dans le code existant :

- absence de fuite de secrets dans les logs ;
- vérification des permissions et des chemins d'accès aux fichiers ;
- validation stricte des entrées API ;
- vérification de la clé d'hôte SSH ;
- limites de taille et de durée des opérations ;
- distinction entre artefact généré, artefact transféré et configuration appliquée ;
- absence de succès déclaré sans preuve correspondante.

Corrige seulement les défauts confirmés. N'entreprends pas une refonte générale.

## Étape 7 — Validation finale obligatoire

Après les modifications :

```bash
go test -count=1 ./...
make fmt-check
make vet
make build
git diff --check
git status --short --branch
git diff --stat
```

Exécute également les tests ciblés et les validations externes qui sont réellement disponibles.

Ne présente pas une commande non exécutée comme réussie. Si une vérification échoue, analyse le résultat et corrige la cause uniquement si elle entre dans le périmètre.

## Livrable final

Fournis un rapport structuré :

1. État initial et défauts confirmés.
2. Modifications effectuées, avec les fichiers concernés.
3. Tests exécutés et résultats exacts.
4. Contrôles non exécutés et raisons.
5. Améliorations de sécurité et de compatibilité.
6. Commandes pour reproduire la démonstration.
7. Risques et limites restants.
8. Évaluation QA argumentée avant/après, avec les preuves qui justifient chaque amélioration.

## Critère d'acceptation

La mission est réussie lorsque les améliorations sont réellement présentes dans le code, que les tests de régression passent, que les sorties restent reproductibles et que la documentation correspond au comportement réel.

L'objectif est de gagner en qualité démontrable, pas en volume de code. Arrête-toi lorsque les critères sont satisfaits. N'entame pas une seconde phase de fonctionnalités sans validation du Product Owner / QA.