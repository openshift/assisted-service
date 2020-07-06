import expirer
import freezegun
import mock
import time
import unittest


FROZEN_TIME = '2020-07-06 21:00:00'
FROZEN_EPOCH = 1594069200

class TestExpirer(unittest.TestCase):
    @freezegun.freeze_time(FROZEN_TIME)
    def test_handle_object_expired(self):
        client = mock.MagicMock()
        client.get_object_tagging.return_value = {'TagSet': [{'Key': 'create_sec_since_epoch', 'Value': str(FROZEN_EPOCH-3700)}]}
        obj ={'Key': 'discovery-image-adfdf1a5-8c77-4746-822e-868859ffbdf2'}
        expirer.handle_object(client, obj)
        client.delete_object.assert_called_with(Bucket='test', Key='discovery-image-adfdf1a5-8c77-4746-822e-868859ffbdf2')

    @freezegun.freeze_time(FROZEN_TIME)
    def test_handle_object_not_expired(self):
        client = mock.MagicMock()
        client.get_object_tagging.return_value = {'TagSet': [{'Key': 'create_sec_since_epoch', 'Value': str(FROZEN_EPOCH-500)}]}
        obj ={'Key': 'discovery-image-adfdf1a5-8c77-4746-822e-868859ffbdf2'}
        expirer.handle_object(client, obj)
        assert(not client.delete_object.called)


if __name__ == '__main__':
    unittest.main()
