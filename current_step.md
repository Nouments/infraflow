2. La prochaine étape après ce prompt

Une fois la phase 1 terminée et vérifiée, je recommande de passer à un premier adaptateur réseau réel, plutôt que d’essayer de supporter simultanément Cisco, FortiGate, MikroTik et Proxmox.

Phase 2 — Premier adaptateur réseau réel

Étape suivante

Choisir un équipement disponible dans ton laboratoire GNS3/EVE-NG, détecter son état réel, exécuter une opération contrôlée, puis vérifier le résultat.

Phase 3 — Cycle YAML → plan → exécution → vérification

Chaque action doit posséder un statut explicite, un résultat observé et une preuve. La génération d’un fichier ne doit jamais être confondue avec l’application de sa configuration.

Phase 4 — Démonstration complète

Afficher dans la TUI et le dashboard les équipements, les jobs, la progression, les logs, les erreurs et l’état réellement vérifié.

3. Prompt pour préparer la phase 2

Tu peux le donner aux agents après la validation de la phase 1. Il s’agit d’abord d’un audit et d’une préparation, pas d’une autorisation d’implémenter tous les constructeurs.

INFRAFLOW — PHASE 2 : PREMIER ADAPTATEUR RÉSEAU RÉEL
INFRAFLOW — PHASE 2 : PREMIER ADAPTATEUR RÉSEAU RÉEL

Travaillez exclusivement sur develop.

Objectif : identifier et implémenter le premier chemin d’exécution réseau réel supporté par InfraFlow, à partir des capacités effectivement présentes dans le dépôt et du laboratoire disponible.

Avant toute modification :

Auditez les interfaces de providers/adapters, le moteur de planification, les jobs, les états d’exécution, les mécanismes de vérification et les tests existants.
Identifiez les équipements réellement disponibles et leurs moyens d’accès : console, SSH, API ou protocole adapté.
Vérifiez les capacités effectivement implémentées pour chaque constructeur. Ne déduisez pas le support d’un constructeur de la seule présence de son nom dans un YAML.
Choisissez un seul équipement cible et justifiez ce choix.
Proposez un scénario minimal, reproductible, non destructif et vérifiable.

Le premier adaptateur devra distinguer au minimum :

configuration absente ;
connexion impossible ;
équipement détecté ;
capacité non prise en charge ;
plan généré ;
action exécutée ;
action échouée ;
résultat vérifié ;
vérification impossible.

Chaque action devra être liée à un équipement, une tâche et un run. Enregistrez les résultats et les erreurs dans les mécanismes d’observabilité existants.

N’annoncez jamais une configuration appliquée si seule sa génération a été réalisée. Ne marquez jamais une action comme vérifiée sans une lecture ou une preuve indépendante adaptée.

Exigences de sécurité :

secrets exclus des logs ;
validation stricte des entrées ;
délais d’attente ;
annulation ;
gestion des erreurs de connexion ;
privilèges minimaux ;
opérations non destructives par défaut ;
tests avec équipements simulés séparés des tests de laboratoire réels.

Livrables attendus :

état des lieux de l’architecture existante ;
choix du premier équipement cible et prérequis ;
scénario de test reproductible ;
fichiers à modifier ;
tests unitaires et tests d’intégration ;
preuves requises pour valider une action réelle ;
critères d’acceptation ;
limitations et fonctionnalités non supportées.

Ne commencez pas une prise en charge générique de tous les constructeurs. Aucun déploiement destructif, aucune modification d’équipement réel et aucun push sans autorisation explicite.