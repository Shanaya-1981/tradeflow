from locust import HttpUser, task, between, constant_throughput
import random
import string

GATEWAY_URL = "http://tradeflow-alb-652183232.us-west-2.elb.amazonaws.com"

def generate_fix_message():
    """Generate a realistic FIX new order message with random quantity and price."""
    sender = "CLIENT"
    broker = "BROKER"
    # mix of small and large orders so both queues get exercised
    # ~30% of orders will be large (qty > 1000) hitting priority queue
    # ~70% will be small hitting standard queue
    quantity = random.choice([
        random.randint(100, 999),    # small order — standard queue
        random.randint(100, 999),
        random.randint(1001, 10000), # large order — priority queue
    ])
    price = round(random.uniform(10.0, 500.0), 2)
    side = random.choice(["1", "2"])  # 1=BUY, 2=SELL
    seq_num = random.randint(1, 9999)

    return (
        f"8=FIX.4.2|"
        f"35=D|"
        f"49={sender}|"
        f"56={broker}|"
        f"34={seq_num}|"
        f"54={side}|"
        f"38={quantity}|"
        f"44={price}"
    )


class FIXUser(HttpUser):
    """
    Base user class. Sends FIX orders to the gateway.
    Spawn rate and user count controlled via Locust CLI or UI.
    """
    wait_time = between(0.1, 0.5)

    @task
    def send_order(self):
        payload = {"message": generate_fix_message()}
        with self.client.post(
            "/order",
            json=payload,
            catch_response=True
        ) as response:
            if response.status_code == 202:
                response.success()
            else:
                response.failure(f"unexpected status: {response.status_code}")


class RampUser(HttpUser):
    """
    Experiment 1 — throughput vs dollar cost.
    Run with increasing --users flag: 10, 100, 1000
    locust -f locustfile.py RampUser --users 10 --spawn-rate 10 --run-time 2m
    locust -f locustfile.py RampUser --users 100 --spawn-rate 10 --run-time 2m
    locust -f locustfile.py RampUser --users 1000 --spawn-rate 50 --run-time 2m
    """
    wait_time = between(0.05, 0.1)

    @task
    def send_order(self):
        payload = {"message": generate_fix_message()}
        self.client.post("/order", json=payload)


class MarketOpenUser(HttpUser):
    """
    Experiment 4 — market open spike simulation.
    Flat low load for 5 minutes then sudden spike.
    Run two times:
      Run 1: reactive  — no pre-scaling
      Run 2: predictive — trigger ECS scale-out before starting this
    locust -f locustfile.py MarketOpenUser --users 500 --spawn-rate 500 --run-time 10m
    """
    # constant_throughput controls messages per second per user
    wait_time = constant_throughput(1)

    @task(1)
    def send_low_load(self):
        """Runs during the first 5 minutes — low traffic."""
        payload = {"message": generate_fix_message()}
        self.client.post("/order", json=payload)

    @task(10)
    def send_spike(self):
        """
        Weight 10 simulates the spike — 10x more likely than low load.
        Locust picks up this weight after warm-up period.
        Swap task weights manually at T=5min to simulate 9:30am.
        """
        payload = {"message": generate_fix_message()}
        self.client.post("/order", json=payload)


class ConcurrentSessionUser(HttpUser):
    """
    Experiment 3 — sequence number violations under concurrency.
    Each user maintains its own sequence counter simulating a FIX session.
    Run with high spawn rate to maximise concurrent sessions.
    locust -f locustfile.py ConcurrentSessionUser --users 50 --spawn-rate 50 --run-time 5m
    """
    wait_time = between(0.01, 0.05)

    def on_start(self):
        """Each user gets its own session ID and sequence counter."""
        self.seq_num = 1
        self.session_id = ''.join(random.choices(string.ascii_uppercase, k=6))

    @task
    def send_ordered_message(self):
        """Send messages with incrementing sequence numbers per session."""
        quantity = random.randint(100, 5000)
        price = round(random.uniform(10.0, 500.0), 2)
        message = (
            f"8=FIX.4.2|"
            f"35=D|"
            f"49={self.session_id}|"
            f"56=BROKER|"
            f"34={self.seq_num}|"  # strict sequence per session
            f"54=1|"
            f"38={quantity}|"
            f"44={price}"
        )
        self.seq_num += 1
        payload = {"message": message}
        self.client.post("/order", json=payload)