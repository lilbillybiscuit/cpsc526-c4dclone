import os
import requests
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class C4DRecovery:
    def __init__(self, agent_url):
        """
        Initialize the C4DRecovery library.
        :param agent_url: Base URL of the C4D agent server (e.g., "http://monitor:8081")
        """
        self.agent_url = agent_url

    def fetch_env_vars(self):
        """
        Fetch environment variables from the C4D agent.
        :return: A dictionary of environment variables.
        """
        try:
            response = requests.get(f"{self.agent_url}/env")
            response.raise_for_status()
            env_vars = response.json()
            logger.info("Fetched environment variables from C4D agent.")
            return env_vars
        except requests.RequestException as e:
            logger.error(f"Failed to fetch environment variables: {e}")
            raise

    def report_event(self, event_type, details):
        """
        Report notable events to the C4D agent.
        :param event_type: The type of event (e.g., "crash", "warning", "info").
        :param details: A detailed message about the event.
        """
        payload = {"event_type": event_type, "details": details}
        try:
            response = requests.post(f"{self.agent_url}/log", json=payload)
            response.raise_for_status()
            logger.info(f"Reported event: {event_type} - {details}")
        except requests.RequestException as e:
            logger.error(f"Failed to report event: {e}")

    def assign_rank(self, node_id):
        """
        Fetch the rank for the node from the C4D agent.
        :param node_id: Unique identifier for the node.
        :return: The assigned rank and world size.
        """
        try:
            response = requests.get(f"{self.agent_url}/rank/{node_id}")
            response.raise_for_status()
            rank_info = response.json()
            logger.info(f"Assigned rank: {rank_info['rank']} for node {node_id}")
            return rank_info["rank"], rank_info["world_size"]
        except requests.RequestException as e:
            logger.error(f"Failed to assign rank: {e}")
            raise

    def offload_model_state(self, source_node_id, target_node_id):
        """
        Offload the model state from a failing node to another healthy node.
        """
        payload = {"source_node_id": source_node_id, "target_node_id": target_node_id}
        try:
            response = requests.post(f"{self.agent_url}/offload", json=payload)
            response.raise_for_status()
            logger.info(f"Model state offloaded from {source_node_id} to {target_node_id}")
        except requests.RequestException as e:
            logger.error(f"Failed to offload model state: {e}")
